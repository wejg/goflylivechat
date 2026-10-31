package controller_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/ws"
)

func TestAutoReplyMetadataJSON(t *testing.T) {
	for _, value := range []interface{}{&models.MessageKefu{}, &ws.ClientMessage{}} {
		if err := json.Unmarshal([]byte(`{"auto_reply_type":"offline"}`), value); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]interface{}
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["auto_reply_type"] != "offline" {
			t.Fatalf("%T lost automatic reply metadata: %s", value, data)
		}
	}
	for _, value := range []interface{}{models.MessageKefu{}, ws.ClientMessage{}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]interface{}
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if _, exists := fields["auto_reply_type"]; exists {
			t.Fatalf("%T labels manual messages as automatic: %s", value, data)
		}
	}
}

func TestAutoReplyMetadataInLiveMessagesAndHistory(t *testing.T) {
	for _, kind := range []string{"offline", "keyword"} {
		t.Run(kind, func(t *testing.T) {
			f := offlineReplyFixture(t)
			guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
			if kind == "keyword" {
				reply := models.ReplyItem{UserId: f.from.Name, GroupId: "1", ItemName: "question", Content: "keyword answer"}
				if err := models.DB.Create(&reply).Error; err != nil {
					t.Fatal(err)
				}
			}
			messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "question", "visitor")
			if err != nil {
				t.Fatal(err)
			}
			ws.VisitorAutoReply(f.visitor, f.from, "question", messageID)
			var live ws.ClientMessage
			if err := json.Unmarshal(transferEvent(t, guest, "message"), &live); err != nil {
				t.Fatal(err)
			}
			if live.AutoReplyType != kind {
				t.Fatalf("live reply type=%q want %q", live.AutoReplyType, kind)
			}
			history := f.request(t, "GET", "/2/messagesPages?"+url.Values{"visitor_id": {f.visitor.VisitorId}}.Encode(), f.from.Name, nil)
			if history.Code != 200 || len(history.Result.List) != 2 || history.Result.List[0].AutoReplyType != kind || history.Result.List[1].AutoReplyType != "" {
				t.Fatalf("history lost automatic reply origin: %+v", history)
			}
			messages := models.FindMessageByWhere("message.visitor_id=?", f.visitor.VisitorId)
			if len(messages) != 2 || messages[1].AutoReplyType != kind {
				t.Fatalf("unpaged history lost automatic reply origin: %+v", messages)
			}
			ws.VisitorMessage(f.visitor.VisitorId, live.Content, f.from, 0)
			var manual ws.ClientMessage
			if err := json.Unmarshal(transferEvent(t, guest, "message"), &manual); err != nil {
				t.Fatal(err)
			}
			if manual.AutoReplyType != "" {
				t.Fatalf("same human reply content was labeled automatic: %+v", manual)
			}
		})
	}
}

func offlineReplyFixture(t *testing.T) transferFixture {
	t.Helper()
	f := newTransferFixture(t)
	config := models.Config{UserId: f.from.Name, ConfKey: "OfflineMessage", ConfValue: "offline answer"}
	if err := models.DB.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func offlineReplyCount(t *testing.T, visitorID string) int {
	t.Helper()
	var count int
	if err := models.DB.Model(&models.Message{}).Where("visitor_id=? AND auto_reply_type=? AND mes_type=?", visitorID, "offline", "kefu").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestOnlineAgentDoesNotSendOfflineReminder(t *testing.T) {
	f := offlineReplyFixture(t)
	transferSocket(t, f.from.Name, "", false)
	messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "question", "visitor")
	if err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "question", messageID)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 0 {
		t.Fatalf("online agent produced %d offline reminders", count)
	}
}

func TestAfterHoursReplyForOfflineAgentWithoutOfflineConfig(t *testing.T) {
	f := newTransferFixture(t)
	if common.IsServiceTime(time.Now()) {
		t.Skip("requires running outside service hours")
	}
	for i := 0; i < 2; i++ {
		messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "after-hours question", "visitor")
		if err != nil {
			t.Fatal(err)
		}
		ws.VisitorAutoReply(f.visitor, f.from, "after-hours question", messageID)
	}
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("after-hours messages produced %d reminders", count)
	}
	var reply models.Message
	if err := models.DB.Where("visitor_id=? AND auto_reply_type=?", f.visitor.VisitorId, "offline").First(&reply).Error; err != nil {
		t.Fatal(err)
	}
	if reply.Content != common.ServiceHoursMessage {
		t.Fatalf("unexpected after-hours reminder: %q", reply.Content)
	}
}

func TestOfflineReplyOnceUntilHumanReply(t *testing.T) {
	f := offlineReplyFixture(t)
	for index := 0; index < 3; index++ {
		messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "question", "visitor")
		if err != nil {
			t.Fatal(err)
		}
		visitor := models.FindVisitorByVistorId(f.visitor.VisitorId)
		ws.VisitorAutoReply(visitor, f.from, "question", messageID)
		if count := offlineReplyCount(t, visitor.VisitorId); count != 1 {
			t.Fatalf("message %d produced %d offline replies", index+1, count)
		}
	}
	if err := models.Connect(); err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "question", 0)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("database reconnect lost the reminder state: %d", count)
	}
	reply := models.ReplyItem{UserId: f.from.Name, GroupId: "1", ItemName: "hours", Content: "keyword answer"}
	if err := models.DB.Create(&reply).Error; err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "hours", 0)
	ws.VisitorAutoReply(f.visitor, f.from, "question", 0)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("keyword reply reset the conversation: %d", count)
	}
	if _, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "human answer", "kefu"); err != nil {
		t.Fatal(err)
	}
	messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "new question", "visitor")
	if err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "new question", messageID)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 2 {
		t.Fatalf("human reply did not start a new conversation: %d", count)
	}
	var unread int
	if err := models.DB.Model(&models.Message{}).Where("visitor_id=? AND mes_type=? AND status=?", f.visitor.VisitorId, "visitor", "unread").Count(&unread).Error; err != nil || unread != 4 {
		t.Fatalf("visitor unread state changed: count=%d err=%v", unread, err)
	}
}

func TestConcurrentOfflineMessagesOnlyReplyOnce(t *testing.T) {
	f := offlineReplyFixture(t)
	var workers sync.WaitGroup
	for index := 0; index < 5; index++ {
		content := fmt.Sprintf("question %d", index)
		messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, content, "visitor")
		if err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			ws.VisitorAutoReply(f.visitor, f.from, content, messageID)
		}()
	}
	workers.Wait()
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("concurrent messages produced %d offline replies", count)
	}
}

func TestDelayedOfflineReplyDoesNotFollowHumanReply(t *testing.T) {
	f := offlineReplyFixture(t)
	messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "question", "visitor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "human answer", "kefu"); err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "question", messageID)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 0 {
		t.Fatalf("already answered message produced %d offline replies", count)
	}
}

func TestFailedOfflineReplyCanBeRetried(t *testing.T) {
	f := offlineReplyFixture(t)
	models.DB.Callback().Create().Before("gorm:create").Register("test:fail_offline_reply", func(scope *gorm.Scope) {
		if message, ok := scope.Value.(*models.Message); ok && message.AutoReplyType == "offline" {
			scope.Err(errors.New("offline reply save failed"))
		}
	})
	t.Cleanup(func() { models.DB.Callback().Create().Remove("test:fail_offline_reply") })
	ws.VisitorAutoReply(f.visitor, f.from, "question", 0)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 0 {
		t.Fatalf("failed save persisted %d replies", count)
	}
	models.DB.Callback().Create().Remove("test:fail_offline_reply")
	ws.VisitorAutoReply(f.visitor, f.from, "question", 0)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("failed save consumed the reminder: %d", count)
	}
}

func TestRetryingHumanMessageDoesNotResetOfflineReminder(t *testing.T) {
	f := offlineReplyFixture(t)
	key := "human-" + f.visitor.VisitorId
	humanID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "answer", "kefu", key)
	if err != nil {
		t.Fatal(err)
	}
	messageID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "question", "visitor")
	if err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "question", messageID)
	retriedID, err := models.CreateMessage(f.from.Name, f.visitor.VisitorId, "answer", "kefu", key)
	if err != nil || retriedID != humanID {
		t.Fatalf("human retry: id=%d err=%v", retriedID, err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "question", messageID)
	if count := offlineReplyCount(t, f.visitor.VisitorId); count != 1 {
		t.Fatalf("retry reset the reminder: %d", count)
	}
}
