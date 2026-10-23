package models_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/middleware"
	"goflylivechat/models"
	"goflylivechat/tools"
)

func TestAppVisitorMappingLeaseAndCrossAccountIsolation(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL")
	}
	if models.DB == nil {
		t.Fatal("database unavailable")
	}
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	appID := "test_" + tools.Uuid()
	agent := models.User{Name: "app-agent-" + tools.Uuid(), Nickname: "Agent"}
	if err := models.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		models.DB.Exec("DELETE FROM visitor_session WHERE app_id=?", appID)
		models.DB.Exec("DELETE FROM visitor WHERE visitor_id IN (SELECT visitor_id FROM app_visitor_mapping WHERE app_id=?)", appID)
		models.DB.Exec("DELETE FROM app_visitor_mapping WHERE app_id=?", appID)
		models.DB.Unscoped().Delete(&agent)
	})
	var active atomic.Bool
	active.Store(true)
	var mu sync.Mutex
	used := map[string]bool{}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Support-Key") != "0123456789abcdef0123456789abcdef" {
			w.WriteHeader(401)
			return
		}
		var input map[string]string
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		if r.URL.Path == "/api/internal/support/tickets/consume" {
			mu.Lock()
			seen := used[input["ticket"]]
			used[input["ticket"]] = true
			mu.Unlock()
			if seen {
				w.WriteHeader(401)
				return
			}
			userID := "A"
			if strings.HasPrefix(input["ticket"], "B") {
				userID = "B"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"code": "ok", "data": common.AppIdentity{AppID: appID, UserID: userID, SourceSessionID: "session_" + userID, SourceExpiresAt: time.Now().Add(time.Hour), Name: "👋" + strings.Repeat("访客", 30) + userID}})
			return
		}
		if r.URL.Path != "/api/internal/support/sessions/check" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": "ok", "data": common.AppSessionCheck{Active: active.Load(), ExpiresAt: time.Now().Add(time.Hour)}})
	}))
	defer source.Close()
	t.Setenv("GOFLY_ENV", "development")
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_APP_ID", appID)
	t.Setenv("GOFLY_APP_API_BASE_URL", source.URL)
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOFLY_APP_DEFAULT_AGENT", agent.Name)
	ticket := "A" + tools.Uuid()
	first, cookie, s1, err := models.CreateAppVisitorSession(context.Background(), ticket, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if len([]rune(first.Name)) != 50 || !strings.HasPrefix(first.Name, "👋") {
		t.Fatalf("unicode nickname not preserved: %q", first.Name)
	}
	if stored := models.FindVisitorByVistorId(first.VisitorId); stored.Name != first.Name {
		t.Fatalf("emoji nickname roundtrip failed: %q", stored.Name)
	}
	if s1.Hash == cookie || s1.Hash != models.AppSessionHash(cookie) || len(cookie) != 43 {
		t.Fatal("session token not safely hashed")
	}
	if _, _, _, err = models.CreateAppVisitorSession(context.Background(), ticket, "127.0.0.1"); err == nil {
		t.Fatal("ticket replay accepted")
	}
	var visitors [4]models.Visitor
	var failures [4]error
	var wg sync.WaitGroup
	for i := range visitors {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			visitors[i], _, _, failures[i] = models.CreateAppVisitorSession(context.Background(), "A"+tools.Uuid(), "127.0.0.1")
		}(i)
	}
	wg.Wait()
	for i, v := range visitors {
		if failures[i] != nil || v.VisitorId != first.VisitorId {
			t.Fatalf("concurrent mapping %d=%+v err=%v", i, v, failures[i])
		}
	}
	// Reentry cannot reset a transferred assignment.
	if err = models.DB.Model(&first).Update("to_id", "transferred").Error; err != nil {
		t.Fatal(err)
	}
	again, _, _, err := models.CreateAppVisitorSession(context.Background(), "A"+tools.Uuid(), "127.0.0.1")
	if err != nil || again.ToId != "transferred" {
		t.Fatalf("assignment lost: %+v %v", again, err)
	}
	b, _, _, err := models.CreateAppVisitorSession(context.Background(), "B"+tools.Uuid(), "127.0.0.1")
	if err != nil || b.VisitorId == first.VisitorId {
		t.Fatal("accounts merged", err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/private", middleware.VisitorScope, func(c *gin.Context) { c.JSON(200, gin.H{"visitor": c.GetString("visitor_id")}) })
	req := httptest.NewRequest("GET", "http://chat.local/private?visitor_id="+b.VisitorId, nil)
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: cookie})
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("cross-account access=%d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "http://chat.local/private?visitor_id="+first.VisitorId+"&visitorId="+b.VisitorId, nil)
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: cookie})
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("conflicting visitor aliases bypassed ownership")
	}
	legacy, _ := tools.MakeToken(map[string]interface{}{"visitor_id": b.VisitorId})
	req = httptest.NewRequest("GET", "http://chat.local/private?visitor_id="+b.VisitorId, nil)
	req.Header.Set("token", legacy)
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("legacy visitor JWT bypassed App mode")
	}

	engine.POST("/actor", middleware.ChatActor, func(c *gin.Context) {
		c.JSON(200, gin.H{"visitor": c.GetString("visitor_id"), "agent": c.GetString("kefu_name")})
	})
	agentJWT, _ := tools.MakeToken(map[string]interface{}{"kefu_name": agent.Name, "kefu_id": agent.ID, "create_time": time.Now().Unix()})
	req = httptest.NewRequest("POST", "http://chat.local/actor", nil)
	req.Header.Set("Origin", "http://chat.local")
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: cookie})
	req.AddCookie(&http.Cookie{Name: "gofly_agent", Value: agentJWT})
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var actor map[string]string
	if json.Unmarshal(w.Body.Bytes(), &actor) != nil || actor["visitor"] != first.VisitorId || actor["agent"] != "" {
		t.Fatalf("visitor upload inherited admin cookie: %s", w.Body.String())
	}
	req = httptest.NewRequest("POST", "http://chat.local/actor", nil)
	req.Header.Set("token", agentJWT)
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: cookie})
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	actor = map[string]string{}
	json.Unmarshal(w.Body.Bytes(), &actor)
	if actor["agent"] != agent.Name || actor["visitor"] != "" {
		t.Fatalf("explicit agent token ignored: %s", w.Body.String())
	}

	engine.GET("/attachment", middleware.AttachmentActor, func(c *gin.Context) {
		c.JSON(200, gin.H{"visitor": c.GetString("visitor_id"), "agent": c.GetString("kefu_name")})
	})
	req = httptest.NewRequest("GET", "http://chat.local/attachment", nil)
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: cookie})
	req.AddCookie(&http.Cookie{Name: "gofly_agent", Value: agentJWT})
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	actor = map[string]string{}
	json.Unmarshal(w.Body.Bytes(), &actor)
	if actor["visitor"] != first.VisitorId || actor["agent"] != agent.Name {
		t.Fatalf("attachment lost a legitimate identity: %s", w.Body.String())
	}
	req = httptest.NewRequest("GET", "http://chat.local/attachment", nil)
	req.AddCookie(&http.Cookie{Name: common.AppVisitorCookie, Value: strings.Repeat("z", 43)})
	req.AddCookie(&http.Cookie{Name: "gofly_agent", Value: agentJWT})
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	actor = map[string]string{}
	json.Unmarshal(w.Body.Bytes(), &actor)
	if w.Code != 200 || actor["agent"] != agent.Name || actor["visitor"] != "" {
		t.Fatalf("stale visitor blocked legitimate agent attachment: %s", w.Body.String())
	}
	req = httptest.NewRequest("GET", "http://chat.local/attachment?token="+agentJWT, nil)
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("attachment accepted agent token from URL")
	}
	// An expired verification lease must consult the source and fail closed.
	active.Store(false)
	if err = models.DB.Model(s1).Update("verified_until", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = models.ValidateAppSessionHash(context.Background(), s1.Hash); err == nil {
		t.Fatal("revoked source accepted")
	}
	if models.AppSessionLeaseValid(s1.Hash) {
		t.Fatal("revoked session still has lease")
	}
	if err = models.RevokeAppSession(s1.Hash); err != nil {
		t.Fatal(err)
	}
}
