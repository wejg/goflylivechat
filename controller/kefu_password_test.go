package controller_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/jinzhu/gorm"
	"goflylivechat/models"
	"goflylivechat/tools"
)

func TestPasswordChangeValidatesInputAndPreservesPasswordOnFailure(t *testing.T) {
	name := settingsFixture(t, true)
	user := models.User{Name: name, Password: tools.Md5("old-password")}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ old, next, confirm string }{
		{"", "new-password", "new-password"},
		{"old-password", "", ""},
		{"old-password", "new-password", ""},
		{"old-password", "short", "short"},
		{"old-password", strings.Repeat("p", 73), strings.Repeat("p", 73)},
		{"old-password", strings.Repeat("密", 25), strings.Repeat("密", 25)},
		{"old-password", "new-password", "different-password"},
		{"wrong-password", "new-password", "new-password"},
	} {
		result := settingsRequest(t, name, "POST", "/modifypass", url.Values{
			"old_pass": {tc.old}, "new_pass": {tc.next}, "confirm_new_pass": {tc.confirm},
		})
		if result.Code != 400 {
			t.Errorf("invalid password change returned code %d", result.Code)
		}
		if models.FindUser(name).Password != user.Password {
			t.Fatal("rejected password change modified stored password")
		}
	}
}

func TestPasswordChangePersistsHashForCurrentUserOnly(t *testing.T) {
	name := settingsFixture(t, true)
	for _, legacy := range []bool{true, false} {
		stored := tools.Md5("old-password")
		if !legacy {
			var err error
			stored, err = tools.HashPassword("old-password")
			if err != nil {
				t.Fatal(err)
			}
		}
		user := models.User{Name: name, Password: stored}
		if err := models.DB.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		other := models.User{Name: name + "-other", Password: stored}
		if err := models.DB.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		for _, next := range []string{"八个汉字的新密码", strings.Repeat("p", 72)} {
			result := settingsRequest(t, name, "POST", "/modifypass", url.Values{
				"old_pass": {"old-password"}, "new_pass": {next}, "confirm_new_pass": {next}, "username": {other.Name},
			})
			if result.Code != 200 {
				t.Fatalf("valid change returned code %d", result.Code)
			}
			updated := models.FindUser(name)
			if ok, legacy := tools.CheckPassword(updated.Password, next); !ok || legacy {
				t.Fatal("new password was not stored as bcrypt")
			}
			if ok, _ := tools.CheckPassword(updated.Password, "old-password"); ok {
				t.Fatal("old password still matches")
			}
			if models.FindUser(other.Name).Password != stored {
				t.Fatal("password change modified another user")
			}
			if err := models.DB.Model(&user).Update("password", stored).Error; err != nil {
				t.Fatal(err)
			}
		}
		models.DB.Unscoped().Delete(&user)
		models.DB.Unscoped().Delete(&other)
	}
}

func TestPasswordChangeDoesNotReportSuccessWhenWriteFails(t *testing.T) {
	name := settingsFixture(t, true)
	user := models.User{Name: name, Password: tools.Md5("old-password")}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	models.DB.Callback().Update().Before("gorm:update").Register("test:password_write_failure", func(scope *gorm.Scope) {
		scope.Err(errors.New("test password write failure"))
	})
	defer models.DB.Callback().Update().Remove("test:password_write_failure")
	result := settingsRequest(t, name, "POST", "/modifypass", url.Values{
		"old_pass": {"old-password"}, "new_pass": {"new-password"}, "confirm_new_pass": {"new-password"},
	})
	if result.Code != 500 || models.FindUser(name).Password != user.Password {
		t.Fatal("failed password write reported success or changed password")
	}
}

func TestProfileCannotBypassOldPasswordVerification(t *testing.T) {
	name := settingsFixture(t, true)
	user := models.User{Name: name, Password: tools.Md5("old-password"), Nickname: "Agent"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	result := settingsRequest(t, name, "POST", "/kefuinfo", url.Values{
		"password": {"new-password"}, "nickname": {"Changed"},
	})
	if result.Code != 400 || models.FindUser(name).Password != user.Password {
		t.Fatal("profile update bypassed old password verification")
	}
	result = settingsRequest(t, name, "POST", "/kefuinfo", url.Values{"nickname": {"Changed"}})
	if result.Code != 200 || models.FindUser(name).Nickname != "Changed" || models.FindUser(name).Password != user.Password {
		t.Fatal("profile update failed or modified password")
	}
}
