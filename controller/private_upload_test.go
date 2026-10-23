package controller_test

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/controller"
	"goflylivechat/models"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDownloadChecksOwnerAndRejectsOtherVisitor(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL")
	}
	if err := models.EnsureUploadSchema(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "invoice.txt")
	if err := os.WriteFile(filename, []byte("private invoice"), 0600); err != nil {
		t.Fatal(err)
	}
	row, err := models.RecordPrivateUpload(filename, "invoice.txt", "download-owner", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { models.DB.Delete(&row) })
	for _, tc := range []struct {
		id   string
		want int
	}{{"download-owner", 200}, {"other", 403}, {"", 403}} {
		r := gin.New()
		r.GET("/attachments/:id", func(c *gin.Context) { c.Set("visitor_id", tc.id) }, controller.GetPrivateUpload)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/attachments/"+row.ID, nil))
		if w.Code != tc.want {
			t.Fatalf("actor %q got %d %s", tc.id, w.Code, w.Body.String())
		}
		if tc.want == http.StatusOK && w.Body.String() != "private invoice" {
			t.Fatal("download body missing")
		}
	}
}
