package controller_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"goflylivechat/controller"
	"goflylivechat/models"
)

type settingsResponse struct {
	Code   int             `json:"code"`
	Result json.RawMessage `json:"result"`
}

func settingsFixture(t *testing.T, transaction bool) string {
	t.Helper()
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL test database")
	}
	if models.DB == nil {
		t.Fatal("test database is not connected")
	}
	user := fmt.Sprintf("settings-%d", time.Now().UnixNano())
	original := models.DB
	if transaction {
		models.DB = original.Begin()
		if models.DB.Error != nil {
			t.Fatal(models.DB.Error)
		}
	}
	t.Cleanup(func() {
		if transaction {
			models.DB.Rollback()
		} else {
			original.Where("user_id IN (?)", []string{user, user + "-other"}).Delete(&models.ReplyItem{})
			original.Where("user_id IN (?)", []string{user, user + "-other"}).Delete(&models.ReplyGroup{})
			original.Where("user_id = ?", user).Delete(&models.Config{})
		}
		models.DB = original
		models.LoadConfigs()
	})
	return user
}

func settingsRequest(t *testing.T, user, method, endpoint string, values url.Values) settingsResponse {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(gin.Recovery(), func(c *gin.Context) { c.Set("kefu_name", user) })
	engine.POST("/config", controller.PostConfig)
	engine.POST("/reply", controller.PostReply)
	engine.POST("/reply_content", controller.PostReplyContent)
	engine.POST("/reply_content_save", controller.PostReplyContentSave)
	engine.DELETE("/reply", controller.DelReplyGroup)
	engine.DELETE("/reply_content", controller.DelReplyContent)
	engine.GET("/replys", controller.GetReplys)
	engine.GET("/autoreply", controller.GetAutoReplys)
	engine.POST("/reply_search", controller.PostReplySearch)
	req := httptest.NewRequest(method, endpoint, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%s %s: HTTP %d: %s", method, endpoint, w.Code, w.Body.String())
	}
	var response settingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func settingsGroup(t *testing.T, user string) models.ReplyGroup {
	t.Helper()
	group := models.ReplyGroup{UserId: user, GroupName: "test"}
	if err := models.DB.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	group.Id = ""
	if err := models.DB.Where("user_id = ?", user).Order("id DESC").First(&group).Error; err != nil {
		t.Fatal(err)
	}
	return group
}

func TestSettingsRejectValuesBeyondDatabaseCharacterLimits(t *testing.T) {
	user := settingsFixture(t, true)
	for _, tc := range []struct {
		endpoint string
		values   url.Values
	}{
		{"/config", url.Values{"key": {strings.Repeat("k", 256)}, "value": {"value"}}},
		{"/config", url.Values{"key": {"WelcomeMessage"}, "value": {strings.Repeat("文", 256)}}},
		{"/reply", url.Values{"group_name": {strings.Repeat("组", 51)}}},
		{"/reply_content", url.Values{"group_id": {"1"}, "item_name": {strings.Repeat("题", 51)}, "content": {"content"}}},
		{"/reply_content", url.Values{"group_id": {"1"}, "item_name": {"title"}, "content": {strings.Repeat("文", 1025)}}},
		{"/reply_content_save", url.Values{"reply_id": {"1"}, "reply_title": {strings.Repeat("题", 51)}, "reply_content": {"content"}}},
		{"/reply_content_save", url.Values{"reply_id": {"1"}, "reply_title": {"title"}, "reply_content": {strings.Repeat("文", 1025)}}},
	} {
		result := settingsRequest(t, user, "POST", tc.endpoint, tc.values)
		if result.Code != 400 {
			t.Errorf("%s accepted overlong input: %+v", tc.endpoint, result)
		}
	}
}

func TestSettingsCanClearValuesAndOnlyCacheSuccessfulWrites(t *testing.T) {
	user := settingsFixture(t, false)
	value := strings.Repeat("文", 255)
	for _, next := range []string{"", value, "", "restored", ""} {
		result := settingsRequest(t, user, "POST", "/config", url.Values{"key": {"WelcomeMessage"}, "value": {next}})
		if result.Code != 200 {
			t.Fatalf("save %q failed: %+v", next, result)
		}
		var config models.Config
		if err := models.DB.Where("user_id = ? AND conf_key = ?", user, "WelcomeMessage").First(&config).Error; err != nil {
			t.Fatal(err)
		}
		if config.ConfValue != next || models.FindUserConfig(user, "WelcomeMessage") != next {
			t.Fatalf("value did not reach database and cache: database=%q cache=%q want=%q", config.ConfValue, models.FindUserConfig(user, "WelcomeMessage"), next)
		}
	}
	original := models.DB
	failed := original.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if failed.Error != nil {
		t.Fatal(failed.Error)
	}
	models.DB = failed
	result := settingsRequest(t, user, "POST", "/config", url.Values{"key": {"WelcomeMessage"}, "value": {"must not be cached"}})
	failed.Rollback()
	models.DB = original
	if result.Code == 200 || models.FindUserConfig(user, "WelcomeMessage") != "" {
		t.Fatalf("failed update reported success or changed cache: %+v", result)
	}
}

func TestReplyRejectsDeletedAndOtherUsersGroups(t *testing.T) {
	user := settingsFixture(t, false)
	deleted := settingsGroup(t, user)
	other := settingsGroup(t, user+"-other")
	if err := models.DB.Delete(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{deleted.Id, other.Id} {
		result := settingsRequest(t, user, "POST", "/reply_content", url.Values{"group_id": {id}, "item_name": {"title"}, "content": {"content"}})
		if result.Code == 200 {
			t.Errorf("accepted deleted or foreign group %s", id)
		}
	}
	var count int
	if err := models.DB.Model(&models.ReplyItem{}).Where("user_id = ?", user).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid group created reply items: count=%d error=%v", count, err)
	}
}

func TestReplyListsIgnoreExistingOrphans(t *testing.T) {
	user := settingsFixture(t, true)
	group := settingsGroup(t, user)
	for _, groupID := range []string{group.Id, "2147483647"} {
		if err := models.DB.Create(&models.ReplyItem{GroupId: groupID, UserId: user, ItemName: "title", Content: "searchable"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ method, endpoint string }{{"GET", "/replys"}, {"GET", "/autoreply?user_id=" + user + "&kefu_id=" + user}, {"POST", "/reply_search"}} {
		result := settingsRequest(t, user, tc.method, tc.endpoint, url.Values{"search": {"searchable"}})
		var groups []models.ReplyGroup
		if err := json.Unmarshal(result.Result, &groups); err != nil {
			t.Fatal(err)
		}
		if result.Code != 200 || len(groups) != 1 || len(groups[0].Items) != 1 {
			t.Fatalf("%s did not preserve valid replies: %+v", tc.endpoint, groups)
		}
	}
}

func TestReplyWriteFailuresDoNotReportSuccess(t *testing.T) {
	user := settingsFixture(t, false)
	group := settingsGroup(t, user)
	item := models.ReplyItem{GroupId: group.Id, UserId: user, ItemName: "old", Content: "old"}
	if err := models.DB.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	item.Id = ""
	if err := models.DB.Where("user_id = ?", user).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	original := models.DB
	failed := original.New()
	failed.AddError(errors.New("isolated test write failure"))
	models.DB = failed
	defer func() { models.DB = original }()
	for _, tc := range []struct {
		method, endpoint string
		values           url.Values
	}{
		{"POST", "/reply", url.Values{"group_name": {"group"}}},
		{"POST", "/reply_content", url.Values{"group_id": {group.Id}, "item_name": {"title"}, "content": {"content"}}},
		{"POST", "/reply_content_save", url.Values{"reply_id": {item.Id}, "reply_title": {"title"}, "reply_content": {"content"}}},
		{"DELETE", "/reply_content?id=" + item.Id, nil},
		{"DELETE", "/reply?id=" + group.Id, nil},
	} {
		if result := settingsRequest(t, user, tc.method, tc.endpoint, tc.values); result.Code == 200 {
			t.Errorf("%s %s ignored database failure", tc.method, tc.endpoint)
		}
	}
}

func TestReplyCreationAndGroupDeletionCannotLeaveOrphans(t *testing.T) {
	user := settingsFixture(t, false)
	group := settingsGroup(t, user)
	creating, release := make(chan struct{}), make(chan struct{})
	models.DB.Callback().Create().Before("gorm:create").Register("test:pause_reply_create", func(scope *gorm.Scope) {
		if item, ok := scope.Value.(*models.ReplyItem); ok && item.UserId == user {
			close(creating)
			<-release
		}
	})
	defer models.DB.Callback().Create().Remove("test:pause_reply_create")
	created, deleted := make(chan struct{}), make(chan struct{})
	go func() { models.CreateReplyContent(group.Id, user, "content", "title"); close(created) }()
	select {
	case <-creating:
	case <-time.After(3 * time.Second):
		t.Fatal("reply creation did not start")
	}
	go func() { models.DeleteReplyGroup(group.Id, user); close(deleted) }()
	select {
	case <-deleted:
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-created
	<-deleted
	var orphans int
	if err := models.DB.Table("reply_item AS i").Joins("LEFT JOIN reply_group AS g ON g.id = i.group_id AND g.user_id = i.user_id").Where("i.user_id = ? AND g.id IS NULL", user).Count(&orphans).Error; err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("concurrent create/delete left %d orphan replies", orphans)
	}
}

func TestReplySavesExactCharacterLimitsAndKeepsOtherUsersItems(t *testing.T) {
	user := settingsFixture(t, false)
	groupName, title, content := strings.Repeat("组", 50), strings.Repeat("题", 50), strings.Repeat("文", 1024)
	if result := settingsRequest(t, user, "POST", "/reply", url.Values{"group_name": {groupName}}); result.Code != 200 {
		t.Fatalf("valid group rejected: %+v", result)
	}
	var group models.ReplyGroup
	if err := models.DB.Where("user_id = ?", user).First(&group).Error; err != nil || group.GroupName != groupName {
		t.Fatalf("group was not persisted: %+v, %v", group, err)
	}
	if result := settingsRequest(t, user, "POST", "/reply_content", url.Values{"group_id": {group.Id}, "item_name": {title}, "content": {content}}); result.Code != 200 {
		t.Fatalf("valid reply rejected: %+v", result)
	}
	var item models.ReplyItem
	if err := models.DB.Where("user_id = ?", user).First(&item).Error; err != nil || item.ItemName != title || item.Content != content {
		t.Fatalf("reply was not persisted: %+v, %v", item, err)
	}
	values := url.Values{"reply_id": {item.Id}, "reply_title": {title}, "reply_content": {content}}
	if result := settingsRequest(t, user, "POST", "/reply_content_save", values); result.Code != 200 {
		t.Fatalf("unchanged reply rejected: %+v", result)
	}
	if result := settingsRequest(t, user+"-other", "POST", "/reply_content_save", values); result.Code == 200 {
		t.Fatal("another user's reply reported a successful update")
	}
	if result := settingsRequest(t, user+"-other", "DELETE", "/reply_content?id="+item.Id, nil); result.Code == 200 {
		t.Fatal("another user's reply reported a successful delete")
	}
	if result := settingsRequest(t, user, "DELETE", "/reply?id="+group.Id, nil); result.Code != 200 {
		t.Fatalf("group delete failed: %+v", result)
	}
	var count int
	if err := models.DB.Model(&models.ReplyItem{}).Where("user_id = ?", user).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("group delete left replies: count=%d error=%v", count, err)
	}
}
