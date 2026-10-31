package models

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jinzhu/gorm"
	"goflylivechat/common"
)

const appPushSchema = `CREATE TABLE IF NOT EXISTS app_push_outbox (
 message_id bigint unsigned NOT NULL PRIMARY KEY,
 app_id varchar(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 user_id varchar(100) NOT NULL,
 visitor_id varchar(100) NOT NULL,
 attempts int NOT NULL DEFAULT 0,
 created_at datetime(6) NOT NULL,
 next_attempt_at datetime(6) NOT NULL,
 KEY app_push_due(app_id,next_attempt_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

type appPushJob struct {
	MessageID     uint
	AppID         string
	UserID        string
	VisitorID     string
	Attempts      int
	CreatedAt     time.Time
	NextAttemptAt time.Time
}

// queueAppPush shares the chat transaction, so unsaved messages never notify.
func queueAppPush(db *gorm.DB, message *Message) error {
	if !common.AppPushEnabled() {
		return nil
	}
	var identity struct {
		AppID  string
		UserID string
	}
	err := db.Table("app_visitor_mapping").Where("visitor_id=? AND app_id=?", message.VisitorId, os.Getenv("GOFLY_APP_ID")).Take(&identity).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	return db.Exec("INSERT INTO app_push_outbox(message_id,app_id,user_id,visitor_id,created_at,next_attempt_at) VALUES(?,?,?,?,?,?)", message.ID, identity.AppID, identity.UserID, message.VisitorId, now, now).Error
}

// DeliverAppPush processes a bounded batch. The lease also survives process exit.
func DeliverAppPush(ctx context.Context) error {
	if !common.AppPushEnabled() {
		return nil
	}
	if DB == nil {
		return errors.New("App push database unavailable")
	}
	now := time.Now()
	appID := os.Getenv("GOFLY_APP_ID")
	if err := DB.Exec("DELETE FROM app_push_outbox WHERE app_id=? AND created_at<? LIMIT 100", appID, now.Add(-24*time.Hour)).Error; err != nil {
		return err
	}
	var jobs []appPushJob
	if err := DB.Table("app_push_outbox").Where("app_id=? AND next_attempt_at<=? AND attempts<16", appID, now).Order("message_id").Limit(10).Find(&jobs).Error; err != nil {
		return err
	}
	var failed bool
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		claim := DB.Exec("UPDATE app_push_outbox SET next_attempt_at=?,attempts=attempts+1 WHERE message_id=? AND next_attempt_at<=? AND attempts=?", time.Now().Add(30*time.Second), job.MessageID, now, job.Attempts)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			continue
		}
		var result struct {
			Accepted bool `json:"accepted"`
		}
		err := common.AppSourceRequest(ctx, "/api/internal/support/notifications", map[string]string{"app_id": job.AppID, "user_id": job.UserID, "message_id": strconv.FormatUint(uint64(job.MessageID), 10)}, &result)
		if err == nil && result.Accepted {
			if err := DB.Exec("DELETE FROM app_push_outbox WHERE message_id=?", job.MessageID).Error; err != nil {
				return err
			}
			continue
		}
		failed = true
		delay := time.Duration(1<<uint(job.Attempts)) * 5 * time.Second
		if delay > time.Hour {
			delay = time.Hour
		}
		if err := DB.Exec("UPDATE app_push_outbox SET next_attempt_at=? WHERE message_id=?", time.Now().Add(delay), job.MessageID).Error; err != nil {
			return err
		}
	}
	if failed {
		return errors.New("App push delivery failed; saved for retry")
	}
	return nil
}

func RunAppPush(ctx context.Context) {
	if !common.AppPushEnabled() {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := DeliverAppPush(ctx); err != nil && ctx.Err() == nil {
			log.Println("App push:", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
