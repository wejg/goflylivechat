package models_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"goflylivechat/models"
	"goflylivechat/tools"
)

func TestAppPushQueuesSavedAgentRepliesOnly(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires disposable MySQL")
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_APP_PUSH_ENABLED", "true")
	t.Setenv("GOFLY_APP_ID", "push_test")
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	visitor := models.Visitor{VisitorId: "push_" + tools.Uuid(), Name: "Push", ToId: "push_agent"}
	if err := models.DB.Create(&visitor).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		models.DB.Exec("DELETE FROM app_push_outbox WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Exec("DELETE FROM message WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Exec("DELETE FROM app_visitor_mapping WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Unscoped().Delete(&visitor)
	})
	if err := models.DB.Exec("INSERT INTO app_visitor_mapping(app_id,user_id,visitor_id) VALUES('push_test','123',?)", visitor.VisitorId).Error; err != nil {
		t.Fatal(err)
	}
	id, err := models.CreateMessage("push_agent", visitor.VisitorId, "secret reply", "kefu", "push_"+tools.Uuid())
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := models.DB.Table("app_push_outbox").Where("message_id=? AND user_id='123' AND app_id='push_test'", id).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("saved reply missing durable push: count=%d err=%v", count, err)
	}
	if _, err = models.CreateMessage("push_agent", visitor.VisitorId, "my question", "visitor"); err != nil {
		t.Fatal(err)
	}
	if err = models.DB.Table("app_push_outbox").Where("visitor_id=?", visitor.VisitorId).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("self message generated push count=%d err=%v", count, err)
	}
	if _, err = models.CreateAutoReply("push_agent", visitor.VisitorId, "auto answer", "keyword", 0); err != nil {
		t.Fatal(err)
	}
	if err = models.DB.Table("app_push_outbox").Where("visitor_id=?", visitor.VisitorId).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("auto reply missing push count=%d err=%v", count, err)
	}
	t.Setenv("GOFLY_APP_PUSH_ENABLED", "false")
	if _, err = models.CreateMessage("push_agent", visitor.VisitorId, "disabled", "kefu"); err != nil {
		t.Fatal(err)
	}
	if err = models.DB.Table("app_push_outbox").Where("visitor_id=?", visitor.VisitorId).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("disabled generated push count=%d err=%v", count, err)
	}
}

func TestAppPushDeliveryRetriesAndAcknowledges(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires disposable MySQL")
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_APP_PUSH_ENABLED", "true")
	t.Setenv("GOFLY_APP_ID", "push_delivery_test")
	t.Setenv("GOFLY_ENV", "development")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	visitor := models.Visitor{VisitorId: "push_" + tools.Uuid(), Name: "Push", ToId: "push_agent"}
	if err := models.DB.Create(&visitor).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		models.DB.Exec("DELETE FROM app_push_outbox WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Exec("DELETE FROM message WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Exec("DELETE FROM app_visitor_mapping WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Unscoped().Delete(&visitor)
	})
	if err := models.DB.Exec("INSERT INTO app_visitor_mapping(app_id,user_id,visitor_id) VALUES('push_delivery_test','123',?)", visitor.VisitorId).Error; err != nil {
		t.Fatal(err)
	}
	key := "push_" + tools.Uuid()
	id, err := models.CreateMessage("push_agent", visitor.VisitorId, "private reply", "kefu", key)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := models.CreateMessage("push_agent", visitor.VisitorId, "private reply", "kefu", key); err != nil || again != id {
		t.Fatalf("idempotency %d %v", again, err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]string
		if r.URL.Path != "/api/internal/support/notifications" || json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload) != 3 || payload["message_id"] != strconv.FormatUint(uint64(id), 10) || payload["user_id"] != "123" || payload["app_id"] != "push_delivery_test" {
			t.Error("incorrect payload")
		}
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"code":"ok","data":{"accepted":true}}`))
	}))
	defer server.Close()
	t.Setenv("GOFLY_APP_API_BASE_URL", server.URL)
	if err := models.DeliverAppPush(context.Background()); err == nil {
		t.Fatal("failure not returned")
	}
	var row struct {
		Attempts      int
		NextAttemptAt time.Time
	}
	if err := models.DB.Table("app_push_outbox").Where("message_id=?", id).Take(&row).Error; err != nil || row.Attempts != 1 || !row.NextAttemptAt.After(time.Now()) {
		t.Fatalf("retry not persisted: %+v %v", row, err)
	}
	if err := models.DeliverAppPush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("ignored backoff")
	}
	if err := models.DB.Exec("UPDATE app_push_outbox SET next_attempt_at=? WHERE message_id=?", time.Now().Add(-time.Second), id).Error; err != nil {
		t.Fatal(err)
	}
	if err := models.DeliverAppPush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := models.DB.Table("app_push_outbox").Where("message_id=?", id).Count(&count).Error; err != nil || count != 0 || calls != 2 {
		t.Fatalf("ack count=%d calls=%d err=%v", count, calls, err)
	}
	// A failed outbox write rolls back the chat reply as well.
	trigger := "push_fail_" + strings.ReplaceAll(tools.Uuid(), "-", "")
	if err := models.DB.Exec("CREATE TRIGGER " + trigger + " BEFORE INSERT ON app_push_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test enqueue failure'").Error; err != nil {
		t.Fatal(err)
	}
	defer models.DB.Exec("DROP TRIGGER " + trigger)
	if _, err := models.CreateMessage("push_agent", visitor.VisitorId, "rollback reply", "kefu"); err == nil {
		t.Fatal("enqueue failure ignored")
	}
	if err := models.DB.Model(&models.Message{}).Where("visitor_id=?", visitor.VisitorId).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("reply escaped rollback count=%d err=%v", count, err)
	}
}

func TestAppPushOrdinaryVisitorDoesNotQueue(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires disposable MySQL")
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_APP_PUSH_ENABLED", "true")
	t.Setenv("GOFLY_APP_ID", "push_test")
	if err := models.EnsureChatSchema(); err != nil {
		t.Fatal(err)
	}
	visitor := models.Visitor{VisitorId: "push_" + tools.Uuid(), Name: "Web", ToId: "push_agent"}
	if err := models.DB.Create(&visitor).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		models.DB.Exec("DELETE FROM message WHERE visitor_id=?", visitor.VisitorId)
		models.DB.Unscoped().Delete(&visitor)
	})
	if _, err := models.CreateMessage("push_agent", visitor.VisitorId, "ordinary reply", "kefu"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := models.DB.Table("app_push_outbox").Where("visitor_id=?", visitor.VisitorId).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("ordinary visitor queued %d %v", count, err)
	}
}
