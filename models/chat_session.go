package models

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/jinzhu/gorm"
	"goflylivechat/common"
)

var ErrAppSession = errors.New("invalid App visitor session")

type AppVisitorSession struct {
	Hash            string     `gorm:"primary_key;size:64" json:"-"`
	VisitorID       string     `gorm:"size:100;not null" json:"-"`
	AppID           string     `gorm:"size:80;not null" json:"-"`
	UserID          string     `gorm:"size:100;not null" json:"-"`
	SourceSessionID string     `gorm:"size:128;not null" json:"-"`
	ExpiresAt       time.Time  `gorm:"not null" json:"-"`
	VerifiedUntil   time.Time  `gorm:"not null" json:"-"`
	RevokedAt       *time.Time `json:"-"`
}

func (AppVisitorSession) TableName() string { return "visitor_session" }

func EnsureChatSchema() error {
	if DB == nil {
		return errors.New("database unavailable")
	}
	var charset string
	if err := DB.Raw("SELECT character_set_name FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='visitor' AND column_name='name'").Row().Scan(&charset); err != nil {
		return err
	}
	if charset != "utf8mb4" {
		if err := DB.Exec("ALTER TABLE visitor MODIFY name varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT ''").Error; err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		"CREATE TABLE IF NOT EXISTS app_visitor_mapping (app_id varchar(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL, user_id varchar(100) CHARACTER SET ascii COLLATE ascii_bin NOT NULL, visitor_id varchar(100) NOT NULL, PRIMARY KEY(app_id,user_id), UNIQUE KEY uniq_app_visitor(visitor_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		"CREATE TABLE IF NOT EXISTS visitor_session (hash varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY, visitor_id varchar(100) NOT NULL, app_id varchar(80) NOT NULL, user_id varchar(100) NOT NULL, source_session_id varchar(128) NOT NULL, expires_at datetime(6) NOT NULL, verified_until datetime(6) NOT NULL, revoked_at datetime(6) NULL, KEY session_visitor(visitor_id), KEY session_expiry(expires_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
	} {
		if err := DB.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func randomAppToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func AppSessionHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func validSourcePart(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func CreateAppVisitorSession(ctx context.Context, ticket, clientIP string) (Visitor, string, *AppVisitorSession, error) {
	var v Visitor
	var identity common.AppIdentity
	verifiedAt := time.Now()
	if len(ticket) < 32 || len(ticket) > 256 {
		return v, "", nil, ErrAppSession
	}
	if err := common.AppSourceRequest(ctx, "/api/internal/support/tickets/consume", map[string]string{"ticket": ticket}, &identity); err != nil {
		return v, "", nil, err
	}
	if identity.AppID != os.Getenv("GOFLY_APP_ID") || !validSourcePart(identity.AppID, 80) || !validSourcePart(identity.UserID, 100) || !validSourcePart(identity.SourceSessionID, 128) || !identity.SourceExpiresAt.After(time.Now()) || len(identity.Name) > 4096 || len(identity.Avatar) > 500 {
		return v, "", nil, ErrAppSession
	}
	if DB == nil {
		return v, "", nil, ErrAppSession
	}
	token, err := randomAppToken()
	if err != nil {
		return v, "", nil, err
	}
	visitorToken, err := randomAppToken()
	if err != nil {
		return v, "", nil, err
	}
	now := time.Now()
	expiry := now.Add(2 * time.Hour)
	if identity.SourceExpiresAt.Before(expiry) {
		expiry = identity.SourceExpiresAt
	}
	lease := verifiedAt.Add(60 * time.Second)
	if expiry.Before(lease) {
		lease = expiry
	}
	session := &AppVisitorSession{Hash: AppSessionHash(token), AppID: identity.AppID, UserID: identity.UserID, SourceSessionID: identity.SourceSessionID, ExpiresAt: expiry, VerifiedUntil: lease}
	tx := DB.Begin()
	if tx.Error != nil {
		return v, "", nil, tx.Error
	}
	defer tx.Rollback()
	if err = tx.Exec("INSERT INTO app_visitor_mapping(app_id,user_id,visitor_id) VALUES(?,?,?) ON DUPLICATE KEY UPDATE visitor_id=visitor_id", identity.AppID, identity.UserID, "app_"+visitorToken).Error; err != nil {
		return v, "", nil, err
	}
	var mapping struct{ VisitorID string }
	if err = tx.Raw("SELECT visitor_id FROM app_visitor_mapping WHERE app_id=? AND user_id=? FOR UPDATE", identity.AppID, identity.UserID).Scan(&mapping).Error; err != nil || mapping.VisitorID == "" {
		return v, "", nil, ErrAppSession
	}
	session.VisitorID = mapping.VisitorID
	err = tx.Where("visitor_id=?", session.VisitorID).First(&v).Error
	if identity.Extra == nil {
		identity.Extra = make(map[string]string)
	}
	identity.Extra["app_id"] = identity.AppID
	identity.Extra["account_id"] = identity.UserID
	extra, marshalErr := json.Marshal(identity.Extra)
	if marshalErr != nil || len(extra) > 8192 {
		return v, "", nil, ErrAppSession
	}
	name := identity.Name
	if runes := []rune(name); len(runes) > 50 {
		name = string(runes[:50])
	}
	if name == "" {
		name = "App visitor"
	}
	avatar := identity.Avatar
	if avatar == "" {
		avatar = "/static/images/2.png"
	}
	if gorm.IsRecordNotFoundError(err) {
		agent := os.Getenv("GOFLY_APP_DEFAULT_AGENT")
		if agent == "" {
			agent = "agent"
		}
		var user User
		if tx.Where("name=?", agent).First(&user).Error != nil {
			return v, "", nil, errors.New("default chat agent unavailable")
		}
		v = Visitor{Name: name, Avator: avatar, ToId: agent, VisitorId: session.VisitorID, Status: 1, ClientIp: clientIP, SourceIp: clientIP, Extra: base64.RawStdEncoding.EncodeToString(extra)}
		if err = tx.Create(&v).Error; err != nil {
			return v, "", nil, err
		}
	} else if err != nil {
		return v, "", nil, err
	} else {
		if err = tx.Model(&v).Updates(map[string]interface{}{"name": name, "avator": avatar, "extra": base64.RawStdEncoding.EncodeToString(extra), "client_ip": clientIP, "status": 1}).Error; err != nil {
			return v, "", nil, err
		}
		v.Name = name
		v.Avator = avatar
		v.Extra = base64.RawStdEncoding.EncodeToString(extra)
	}
	if err = tx.Create(session).Error; err != nil {
		return v, "", nil, err
	}
	if err = tx.Commit().Error; err != nil {
		return v, "", nil, err
	}
	return v, token, session, nil
}

func CurrentAppVisitor(r *http.Request) (*AppVisitorSession, error) {
	if !common.AppChatEnabled() {
		return nil, ErrAppSession
	}
	cookie, err := r.Cookie(common.AppVisitorCookie)
	if err != nil || len(cookie.Value) != 43 {
		return nil, ErrAppSession
	}
	return ValidateAppSessionHash(r.Context(), AppSessionHash(cookie.Value))
}

func ValidateAppSessionHash(ctx context.Context, hash string) (*AppVisitorSession, error) {
	return validateAppSessionHash(ctx, hash, false)
}

// RefreshAppSessionHash checks before the current lease ends, for idle WebSockets.
func RefreshAppSessionHash(ctx context.Context, hash string) (*AppVisitorSession, error) {
	return validateAppSessionHash(ctx, hash, true)
}

func validateAppSessionHash(ctx context.Context, hash string, force bool) (*AppVisitorSession, error) {
	if DB == nil || len(hash) != 64 {
		return nil, ErrAppSession
	}
	var s AppVisitorSession
	if DB.Where("hash=? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now()).First(&s).Error != nil {
		return nil, ErrAppSession
	}
	if !force && time.Now().Before(s.VerifiedUntil) {
		return &s, nil
	}
	var check common.AppSessionCheck
	// The lease starts before the network call, so slow checks never extend a 60s validity window.
	checkedAt := time.Now()
	if err := common.AppSourceRequest(ctx, "/api/internal/support/sessions/check", map[string]string{"app_id": s.AppID, "user_id": s.UserID, "source_session_id": s.SourceSessionID}, &check); err != nil {
		return nil, ErrAppSession
	}
	if !check.Active || !check.ExpiresAt.After(time.Now()) {
		_ = RevokeAppSession(hash)
		return nil, ErrAppSession
	}
	expiry := s.ExpiresAt
	if check.ExpiresAt.Before(expiry) {
		expiry = check.ExpiresAt
	}
	lease := checkedAt.Add(60 * time.Second)
	if expiry.Before(lease) {
		lease = expiry
	}
	result := DB.Model(&AppVisitorSession{}).Where("hash=? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now()).Updates(map[string]interface{}{"verified_until": lease, "expires_at": gorm.Expr("LEAST(expires_at, ?)", expiry)})
	if result.Error != nil {
		return nil, ErrAppSession
	}
	// Re-read to catch concurrent logout (including identical RowsAffected results).
	if DB.Where("hash=? AND revoked_at IS NULL AND expires_at > ?", hash, time.Now()).First(&s).Error != nil || !time.Now().Before(s.VerifiedUntil) {
		return nil, ErrAppSession
	}
	return &s, nil
}

func AppSessionLeaseValid(hash string) bool {
	if DB == nil {
		return false
	}
	var n int
	now := time.Now()
	return DB.Model(&AppVisitorSession{}).Where("hash=? AND revoked_at IS NULL AND expires_at>? AND verified_until>?", hash, now, now).Count(&n).Error == nil && n == 1
}
func RevokeAppSession(hash string) error {
	if DB == nil {
		return ErrAppSession
	}
	return DB.Model(&AppVisitorSession{}).Where("hash=? AND revoked_at IS NULL", hash).Update("revoked_at", time.Now()).Error
}
