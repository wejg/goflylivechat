package ws

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/tools"
)

func TestAppWebSocketCookieExpiryAndReplacement(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL")
	}
	if models.DB == nil {
		t.Fatal("database unavailable")
	}
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_ENV", "development")
	visitor := models.Visitor{VisitorId: "ws-app-" + tools.Uuid(), Name: "WebSocket", ToId: "agent", Status: 1}
	if err := models.DB.Create(&visitor).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if user, ok := ClientList.Get(visitor.VisitorId); ok {
			user.Conn.Close()
		}
		models.DB.Unscoped().Delete(&visitor)
		models.DB.Where("visitor_id=?", visitor.VisitorId).Delete(&models.AppVisitorSession{})
	})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/ws", NewVisitorServer)
	server := httptest.NewServer(engine)
	defer server.Close()
	token := "first" + strings.Repeat("1", 38)
	second := "second" + strings.Repeat("2", 37)
	expiry := time.Now().Add(900 * time.Millisecond)
	for _, value := range []string{token, second} {
		s := models.AppVisitorSession{Hash: models.AppSessionHash(value), VisitorID: visitor.VisitorId, AppID: "test", UserID: "42", SourceSessionID: "source", ExpiresAt: expiry, VerifiedUntil: expiry}
		if err := models.DB.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	dial := func(value string) *websocket.Conn {
		t.Helper()
		header := http.Header{"Origin": {server.URL}, "Cookie": {fmt.Sprintf("%s=%s", common.AppVisitorCookie, value)}}
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?visitor_id="+visitor.VisitorId, header)
		if err != nil {
			t.Fatalf("ws dial: %v response=%v", err, response)
		}
		return conn
	}
	missing, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?visitor_id="+visitor.VisitorId, http.Header{"Origin": {server.URL}})
	if missing != nil {
		missing.Close()
	}
	if err == nil || resp.StatusCode != 401 {
		t.Fatal("unauthenticated websocket accepted")
	}
	one := dial(token)
	defer one.Close()
	two := dial(second)
	defer two.Close()
	one.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err = one.ReadMessage()
		if err != nil {
			break
		}
	}
	current, ok := ClientList.Get(visitor.VisitorId)
	if !ok || current.AppSessionHash != models.AppSessionHash(second) {
		t.Fatal("replacement removed current connection")
	}
	if models.FindVisitorByVistorId(visitor.VisitorId).Status != 1 {
		t.Fatal("old connection marked replacement offline")
	}
	// No client traffic or pushes: the independent watchdog must still close at expiry.
	two.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = two.ReadMessage()
	if err == nil {
		t.Fatal("idle expired websocket stayed open")
	}
	if time.Now().Before(expiry) {
		t.Fatal("replacement socket closed before its own expiry")
	}
}
