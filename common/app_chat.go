package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const AppVisitorCookie = "gofly_visitor"

func AppChatEnabled() bool {
	enabled, _ := strconv.ParseBool(os.Getenv("GOFLY_APP_CHAT_ENABLED"))
	return enabled
}
func ValidateAppChatConfig() error {
	value := os.Getenv("GOFLY_APP_CHAT_ENABLED")
	if value != "" {
		if _, err := strconv.ParseBool(value); err != nil {
			return errors.New("invalid GOFLY_APP_CHAT_ENABLED")
		}
	}
	if !AppChatEnabled() {
		return nil
	}
	if !validAppSecret(os.Getenv("GOFLY_JWT_SECRET")) {
		return errors.New("GOFLY_JWT_SECRET requires a random signing key of at least 32 bytes and 8 distinct characters")
	}
	u, err := url.Parse(os.Getenv("GOFLY_APP_API_BASE_URL"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("GOFLY_APP_API_BASE_URL must be an absolute origin")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && os.Getenv("GOFLY_ENV") == "development") {
		return errors.New("App API requires HTTPS")
	}
	if id := os.Getenv("GOFLY_APP_ID"); id == "" || len(id) > 80 {
		return errors.New("GOFLY_APP_ID is required and must fit 80 bytes")
	}
	if key := os.Getenv("GOFLY_APP_INTEGRATION_KEY"); !validAppSecret(key) {
		return errors.New("GOFLY_APP_INTEGRATION_KEY requires a random key of at least 32 bytes and 8 distinct characters")
	}
	origin := os.Getenv("GOFLY_APP_PUBLIC_ORIGIN")
	if origin == "" && os.Getenv("GOFLY_ENV") != "development" {
		return errors.New("GOFLY_APP_PUBLIC_ORIGIN is required in production")
	}
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && os.Getenv("GOFLY_ENV") == "development")) {
			return errors.New("invalid GOFLY_APP_PUBLIC_ORIGIN")
		}
	}
	return nil
}

func validAppSecret(key string) bool {
	if len(key) < 32 || strings.ContainsAny(key, "\r\n") {
		return false
	}
	distinct := map[rune]bool{}
	for _, c := range key {
		distinct[c] = true
	}
	if len(distinct) < 8 {
		return false
	}
	for _, placeholder := range []string{"change", "replace", "your-secret", "default", "golivechat"} {
		if strings.Contains(strings.ToLower(key), placeholder) {
			return false
		}
	}
	return true
}

// A configured public origin supports TLS termination without trusting forwarded headers.
func AppSameOrigin(r *http.Request) bool {
	origin := os.Getenv("GOFLY_APP_PUBLIC_ORIGIN")
	if origin != "" {
		return r.Header.Get("Origin") == origin
	}
	if os.Getenv("GOFLY_ENV") != "development" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return r.Header.Get("Origin") == scheme+"://"+r.Host
}

type AppIdentity struct {
	AppID           string            `json:"app_id"`
	UserID          string            `json:"user_id"`
	SourceSessionID string            `json:"source_session_id"`
	SourceExpiresAt time.Time         `json:"source_expires_at"`
	Name            string            `json:"name"`
	Avatar          string            `json:"avatar"`
	Extra           map[string]string `json:"extra"`
}

type AppSessionCheck struct {
	Active    bool      `json:"active"`
	ExpiresAt time.Time `json:"expires_at"`
}

func AppSourceRequest(ctx context.Context, path string, input, output interface{}) error {
	if err := ValidateAppChatConfig(); err != nil {
		return err
	}
	if !AppChatEnabled() {
		return errors.New("App chat disabled")
	}
	if path != "/api/internal/support/tickets/consume" && path != "/api/internal/support/sessions/check" {
		return errors.New("invalid App source path")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(os.Getenv("GOFLY_APP_API_BASE_URL"), "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Support-Key", os.Getenv("GOFLY_APP_INTEGRATION_KEY"))
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("App service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("App rejected request")
	}
	limited, err := io.ReadAll(io.LimitReader(resp.Body, 32769))
	if err != nil || len(limited) > 32768 {
		return errors.New("invalid App response")
	}
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(limited, &envelope) != nil || envelope.Code != "ok" || len(envelope.Data) == 0 {
		return errors.New("invalid App envelope")
	}
	return json.Unmarshal(envelope.Data, output)
}
