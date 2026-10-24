package controller_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
	"goflylivechat/models"
	"goflylivechat/ws"
)

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
	if err := models.DB.Model(&models.Message{}).Where("visitor_id=? AND content=? AND mes_type=?", visitorID, "offline answer", "kefu").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
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
