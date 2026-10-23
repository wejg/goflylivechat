package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAppPrivateUploadGuardBlocksCanonicalAndEncodedPaths(t *testing.T) {
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	r := gin.New()
	r.Use(PrivateUploadGuard)
	r.NoRoute(func(c *gin.Context) { c.Status(204) })
	for _, endpoint := range []string{"/static/upload/secret.txt", "/static/upload", "/static/images/../upload/secret.txt", "/static/%75pload/secret.txt"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", endpoint, nil))
		if w.Code != 404 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s allowed: %d", endpoint, w.Code)
		}
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "false")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/static/upload/legacy.txt", nil))
	if w.Code != 204 {
		t.Fatal("legacy mode unexpectedly blocked")
	}
}
