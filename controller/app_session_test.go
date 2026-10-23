package controller_test

import (
	"encoding/json"
	"errors"
	"github.com/jinzhu/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/controller"
	"goflylivechat/models"
	"goflylivechat/tools"
)

func TestAppCookieExchangeRotationAndLogoutIsolation(t *testing.T) {
	f := newTransferFixture(t)
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	appID := "cookie_" + tools.Uuid()
	t.Cleanup(func() {
		models.DB.Exec("DELETE FROM visitor_session WHERE app_id=?", appID)
		models.DB.Exec("DELETE FROM visitor WHERE visitor_id IN (SELECT visitor_id FROM app_visitor_mapping WHERE app_id=?)", appID)
		models.DB.Exec("DELETE FROM app_visitor_mapping WHERE app_id=?", appID)
	})
	var mu sync.Mutex
	consumed := map[string]bool{}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Ticket string `json:"ticket"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seen := consumed[body.Ticket]
		consumed[body.Ticket] = true
		mu.Unlock()
		if seen {
			w.WriteHeader(401)
			return
		}
		userID := "A"
		if strings.HasPrefix(body.Ticket, "B") {
			userID = "B"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": "ok", "data": common.AppIdentity{AppID: appID, UserID: userID, SourceSessionID: "source_" + userID, SourceExpiresAt: time.Now().Add(time.Hour), Name: "Nickname"}})
	}))
	defer source.Close()
	t.Setenv("GOFLY_ENV", "development")
	t.Setenv("GOFLY_APP_PUBLIC_ORIGIN", "")
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_APP_ID", appID)
	t.Setenv("GOFLY_APP_API_BASE_URL", source.URL)
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOFLY_APP_DEFAULT_AGENT", f.from.Name)
	engine := gin.New()
	engine.POST("/visitor/session", controller.PostAppSession)
	engine.GET("/visitor/session", controller.GetAppSession)
	engine.DELETE("/visitor/session", controller.DeleteAppSession)
	request := func(method, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://chat.local/visitor/session", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	ticket := "A" + tools.Uuid()
	body := `{"ticket":"` + ticket + `"}`
	for _, origin := range []string{"", "http://evil.local"} {
		if w := request("POST", body, nil, origin); w.Code != 403 {
			t.Fatalf("unsafe origin %q accepted: %s", origin, w.Body.String())
		}
	}
	w := request("POST", body, nil, "http://chat.local")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	cookie := cookies[0]
	if cookie.Name != common.AppVisitorCookie || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure {
		t.Fatalf("cookie flags: %+v", cookie)
	}
	if strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal("session secret exposed in JSON")
	}
	if w = request("POST", body, nil, "http://chat.local"); w.Code != 401 {
		t.Fatal("replayed ticket accepted")
	}
	if w = request("GET", "", cookie, ""); w.Code != 200 {
		t.Fatal("restore failed", w.Body.String())
	}
	peer := request("POST", `{"ticket":"A`+tools.Uuid()+`"}`, nil, "http://chat.local").Result().Cookies()[0]

	// Failure to revoke the old cookie must not publish the newly minted identity.
	var failed atomic.Bool
	callback := "test:fail_app_old_session_revoke"
	models.DB.Callback().Update().Before("gorm:update").Register(callback, func(scope *gorm.Scope) {
		if scope.TableName() == "visitor_session" && failed.CompareAndSwap(false, true) {
			scope.Err(errors.New("injected old session revocation failure"))
		}
	})
	t.Cleanup(func() { models.DB.Callback().Update().Remove(callback) })
	w = request("POST", `{"ticket":"B`+tools.Uuid()+`"}`, cookie, "http://chat.local")
	models.DB.Callback().Update().Remove(callback)
	if !failed.Load() || w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("revoke failure published replacement: HTTP %d cookies=%v body=%s", w.Code, w.Result().Cookies(), w.Body.String())
	}
	var cancelled []models.AppVisitorSession
	if err := models.DB.Where("app_id=? AND user_id=?", appID, "B").Find(&cancelled).Error; err != nil || len(cancelled) != 1 || cancelled[0].RevokedAt == nil {
		t.Fatalf("replacement session remained usable: rows=%+v err=%v", cancelled, err)
	}
	if request("GET", "", cookie, "").Code != 200 {
		t.Fatal("failed account switch invalidated original cookie")
	}
	// Switching this browser to B revokes its A session, without touching A on another device.
	w = request("POST", `{"ticket":"B`+tools.Uuid()+`"}`, cookie, "http://chat.local")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bCookie := w.Result().Cookies()[0]
	if request("GET", "", cookie, "").Code != 401 {
		t.Fatal("old account cookie survived rotation")
	}
	if request("GET", "", peer, "").Code != 200 {
		t.Fatal("other device revoked by account switch")
	}
	if request("DELETE", "", bCookie, "http://evil.local").Code != 403 {
		t.Fatal("cross-origin logout accepted")
	}
	w = request("DELETE", "", bCookie, "http://chat.local")
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie", w.Body.String())
	}
	if request("GET", "", bCookie, "").Code != 401 {
		t.Fatal("logout cookie still accepted")
	}
	if request("GET", "", peer, "").Code != 200 {
		t.Fatal("logout revoked another session")
	}
}
