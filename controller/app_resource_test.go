package controller_test

import (
	"image"
	"image/png"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"goflylivechat/controller"
	"goflylivechat/models"
)

func TestAppVisitorMetadataRequiresAssignedAgent(t *testing.T) {
	f := newTransferFixture(t)
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	for _, tc := range []struct {
		agent string
		want  int
	}{{f.from.Name, 200}, {f.to.Name, 403}, {"", 403}} {
		r := gin.New()
		r.GET("/visitor", func(c *gin.Context) {
			if tc.agent != "" {
				c.Set("kefu_name", tc.agent)
			}
		}, controller.GetVisitor)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/visitor?visitorId="+f.visitor.VisitorId, nil))
		if w.Code != tc.want {
			t.Fatalf("agent %q got %d: %s", tc.agent, w.Code, w.Body.String())
		}
	}
}

func TestAppAvatarPublishesOnlyOwnNonConversationImage(t *testing.T) {
	f := newTransferFixture(t)
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	if err := models.EnsureUploadSchema(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "avatar.png")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, tc := range []struct {
		visitor, agent string
		want           int
	}{
		{"", f.to.Name, 400}, {f.visitor.VisitorId, f.from.Name, 400}, {"", f.from.Name, 200},
	} {
		row, err := models.RecordPrivateUpload(filename, "avatar.png", tc.visitor, tc.agent)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { models.DB.Delete(&row) })
		r := gin.New()
		r.POST("/modifyavator", func(c *gin.Context) { c.Set("kefu_name", f.from.Name) }, controller.PostKefuAvator)
		req := httptest.NewRequest("POST", "/modifyavator", strings.NewReader(url.Values{"avator": {"/attachments/" + row.ID}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("avatar actor %q/%q got %d %s", tc.visitor, tc.agent, w.Code, w.Body.String())
		}
		if tc.want == 200 {
			published := models.FindUser(f.from.Name).Avator
			if !strings.HasPrefix(published, "/static/avatars/") {
				t.Fatalf("not public avatar: %s", published)
			}
			t.Cleanup(func() { os.Remove(strings.TrimPrefix(published, "/")) })
			if _, err := os.Stat(strings.TrimPrefix(published, "/")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAppEncodedAttachmentReferencesStayInConversation(t *testing.T) {
	f := newTransferFixture(t)
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	if err := models.EnsureUploadSchema(); err != nil {
		t.Fatal(err)
	}
	foreign, err := models.RecordPrivateUpload("not-read-by-this-test.png", "secret.png", "other-conversation", f.from.Name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { models.DB.Delete(&foreign) })
	contents := []string{
		"img[/attachments/" + foreign.ID + "]",
		"img[/%61ttachments/" + foreign.ID + "]",
		"img[/attachments%2F" + foreign.ID + "]",
		"img[/x/../attachments/" + foreign.ID + "]",
		"img[http:attachments/" + foreign.ID + "]",
		"img[https:attachments/" + foreign.ID + "]",
		"img[https:////chat.example/attachments/" + foreign.ID + "]",
		"img[////chat.example/attachments/" + foreign.ID + "]",
		`attachment[{"path":"/\u0061ttachments/` + foreign.ID + `","name":"test"}]`,
	}
	r := gin.New()
	r.POST("/message", func(c *gin.Context) { c.Set("kefu_name", f.from.Name) }, controller.SendKefuMessage)
	for _, content := range contents {
		req := httptest.NewRequest("POST", "/message", strings.NewReader(url.Values{"to_id": {f.visitor.VisitorId}, "content": {content}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("foreign attachment accepted: %q: %d %s", content, w.Code, w.Body.String())
		}
	}
}
