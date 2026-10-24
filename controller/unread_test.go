package controller_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"goflylivechat/models"
	"goflylivechat/router"
	"goflylivechat/tools"
)

type unreadResponse struct {
	Code   int `json:"code"`
	Result struct {
		Unread        uint            `json:"unread"`
		Total         uint            `json:"total"`
		Counts        map[string]uint `json:"counts"`
		LastMessageID uint            `json:"last_message_id"`
	} `json:"result"`
}

func TestMarkAllReadIsScopedAndPreservesNewMessages(t *testing.T) {
	engine, messages := unreadFixture(t)
	summary := unreadRequest(t, engine, "GET", "/message_unread", nil, false)
	if summary.Result.LastMessageID != messages[4].ID {
		t.Fatalf("unexpected read boundary: %+v", summary)
	}
	newMessage := models.Message{KefuId: "unread-agent", VisitorId: "unread-a", Content: "later", MesType: "visitor", Status: "unread"}
	if err := models.DB.Create(&newMessage).Error; err != nil {
		t.Fatal(err)
	}
	response := unreadRequest(t, engine, "POST", "/message_read_all", url.Values{"last_message_id": {fmt.Sprint(summary.Result.LastMessageID)}}, false)
	if response.Code != 200 {
		t.Fatalf("mark all failed: %+v", response)
	}
	for index, message := range append(messages, newMessage) {
		var stored models.Message
		if err := models.DB.First(&stored, message.ID).Error; err != nil {
			t.Fatal(err)
		}
		want := "read"
		if index == 2 || index == 3 || index == 5 {
			want = "unread"
		}
		if stored.Status != want {
			t.Fatalf("message %d status=%s want=%s", stored.ID, stored.Status, want)
		}
	}
	summary = unreadRequest(t, engine, "GET", "/message_unread", nil, false)
	if summary.Result.Total != 1 || summary.Result.Counts["unread-a"] != 1 {
		t.Fatalf("new message was cleared: %+v", summary)
	}
}

func TestMarkAllReadUsesCurrentVisitorOwnership(t *testing.T) {
	engine, messages := unreadFixture(t)
	if err := models.DB.Model(&models.Visitor{}).Where("visitor_id=?", "unread-a").Update("to_id", "other-agent").Error; err != nil {
		t.Fatal(err)
	}
	response := unreadRequest(t, engine, "POST", "/message_read_all", url.Values{"last_message_id": {fmt.Sprint(messages[4].ID)}}, false)
	if response.Code != 200 {
		t.Fatalf("mark all failed: %+v", response)
	}
	var stored models.Message
	models.DB.First(&stored, messages[0].ID)
	if stored.Status != "unread" {
		t.Fatal("transferred visitor was marked read")
	}
}

func TestMarkAllReadRejectsInvalidBoundaryAndVisitorTokens(t *testing.T) {
	engine, _ := unreadFixture(t)
	for _, boundary := range []string{"", "0", "-1", "invalid", "4294967296"} {
		response := unreadRequest(t, engine, "POST", "/message_read_all", url.Values{"last_message_id": {boundary}}, false)
		if response.Code != 400 {
			t.Fatalf("boundary %q accepted: %+v", boundary, response)
		}
	}
	response := unreadRequest(t, engine, "POST", "/message_read_all", url.Values{"last_message_id": {"999999"}}, true)
	if response.Code != 400 {
		t.Fatalf("visitor token accepted: %+v", response)
	}
	summary := unreadRequest(t, engine, "GET", "/message_unread", nil, false)
	if summary.Result.Total != 3 {
		t.Fatalf("invalid request changed unread state: %+v", summary)
	}
}

func unreadFixture(t *testing.T) (*gin.Engine, []models.Message) {
	t.Helper()
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL test database")
	}
	if models.DB == nil {
		t.Fatal("test database is not connected")
	}
	original := models.DB
	tx := original.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	models.DB = tx
	t.Cleanup(func() { tx.Rollback(); models.DB = original })
	visitors := []models.Visitor{
		{VisitorId: "unread-a", ToId: "unread-agent", Name: "A"},
		{VisitorId: "unread-b", ToId: "other-agent", Name: "B"},
		{VisitorId: "unread-offline", ToId: "unread-agent", Name: "Offline"},
	}
	for i := range visitors {
		if err := tx.Create(&visitors[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	messages := []models.Message{
		{KefuId: "unread-agent", VisitorId: "unread-a", Content: "first", MesType: "visitor", Status: "unread"},
		{KefuId: "unread-agent", VisitorId: "unread-a", Content: "second", MesType: "visitor", Status: "unread"},
		{KefuId: "unread-agent", VisitorId: "unread-a", Content: "reply", MesType: "kefu", Status: "unread"},
		{KefuId: "other-agent", VisitorId: "unread-b", Content: "private", MesType: "visitor", Status: "unread"},
		{KefuId: "unread-agent", VisitorId: "unread-offline", Content: "offline", MesType: "visitor", Status: "unread"},
	}
	for i := range messages {
		if err := tx.Create(&messages[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	router.InitApiRouter(engine)
	return engine, messages
}

func unreadRequest(t *testing.T, engine *gin.Engine, method, endpoint string, values url.Values, visitor bool) unreadResponse {
	t.Helper()
	claims := map[string]interface{}{"kefu_name": "unread-agent", "kefu_id": 1, "create_time": time.Now().Unix()}
	if visitor {
		claims = map[string]interface{}{"visitor_id": "unread-a"}
	}
	token, err := tools.MakeToken(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, endpoint, strings.NewReader(values.Encode()))
	req.Header.Set("token", token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%s %s: HTTP %d: %s", method, endpoint, w.Code, w.Body.String())
	}
	var response unreadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestUnreadSummaryIsScopedAndIncludesOfflineVisitors(t *testing.T) {
	engine, _ := unreadFixture(t)
	result := unreadRequest(t, engine, "GET", "/message_unread", nil, false)
	if result.Code != 200 || result.Result.Total != 3 || result.Result.Counts["unread-a"] != 2 || result.Result.Counts["unread-offline"] != 1 || result.Result.Counts["unread-b"] != 0 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	if err := models.DB.Model(&models.Visitor{}).Where("visitor_id=?", "unread-a").Update("to_id", "other-agent").Error; err != nil {
		t.Fatal(err)
	}
	result = unreadRequest(t, engine, "GET", "/message_unread", nil, false)
	if result.Result.Total != 1 || result.Result.Counts["unread-a"] != 0 {
		t.Fatalf("transferred visitor leaked: %+v", result)
	}
}

func TestUnreadStatusSeparatesMessageDirections(t *testing.T) {
	engine, _ := unreadFixture(t)
	agent := unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, false)
	guest := unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, true)
	if agent.Result.Unread != 2 || guest.Result.Unread != 1 {
		t.Fatalf("agent=%+v visitor=%+v", agent, guest)
	}
}

func TestMarkReadOnlyAcknowledgesDisplayedIncomingMessages(t *testing.T) {
	engine, messages := unreadFixture(t)
	response := unreadRequest(t, engine, "POST", "/message_status", url.Values{"visitor_id": {"unread-a"}, "last_message_id": {fmt.Sprint(messages[0].ID)}}, false)
	if response.Code != 200 {
		t.Fatalf("read failed: %+v", response)
	}
	agent := unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, false)
	guest := unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, true)
	if agent.Result.Unread != 1 || guest.Result.Unread != 1 {
		t.Fatalf("unseen message or opposite direction cleared: agent=%+v guest=%+v", agent, guest)
	}
	response = unreadRequest(t, engine, "POST", "/message_status", url.Values{"visitor_id": {"unread-a"}, "last_message_id": {fmt.Sprint(messages[2].ID)}}, true)
	if response.Code != 200 {
		t.Fatalf("visitor read failed: %+v", response)
	}
	agent = unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, false)
	if agent.Result.Unread != 1 {
		t.Fatalf("visitor cleared agent unread: %+v", agent)
	}
}

func TestMarkReadRejectsInvalidBoundaryAndOtherAgentsVisitor(t *testing.T) {
	engine, _ := unreadFixture(t)
	for _, boundary := range []string{"", "0", "-1", "oops"} {
		result := unreadRequest(t, engine, "POST", "/message_status", url.Values{"visitor_id": {"unread-a"}, "last_message_id": {boundary}}, false)
		if result.Code != 400 {
			t.Fatalf("boundary %q accepted: %+v", boundary, result)
		}
	}
	denied := unreadRequest(t, engine, "POST", "/message_status", url.Values{"visitor_id": {"unread-b"}, "last_message_id": {"999999"}}, false)
	if denied.Code != 403 {
		t.Fatalf("another agent's visitor was writable: %+v", denied)
	}
	unchanged := unreadRequest(t, engine, "GET", "/message_status?visitor_id=unread-a", nil, false)
	if unchanged.Result.Unread != 2 {
		t.Fatalf("invalid request changed unread state: %+v", unchanged)
	}
}
