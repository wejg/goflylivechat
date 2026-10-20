package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"goflylivechat/tools"
)

func init() { gin.SetMode(gin.TestMode) }

func TestShouldCompress(t *testing.T) {
	cases := map[string]bool{
		"/static/js/functions.js":  true,
		"/static/css/common.css":   true,
		"/livechat":                true,
		"/2/messagesPages":         true,
		"/static/images/1.png":     false,
		"/static/images/alert.mp3": false,
		"/static/cdn/a.woff":       false,
	}
	for path, want := range cases {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if got := shouldCompress(req); got != want {
			t.Errorf("%s: 期望 %v，得到 %v", path, want, got)
		}
	}
}

// 没有 Accept-Encoding: gzip 时必须原样返回，不能加 Content-Encoding 头。
func TestGzipSkippedWithoutAcceptEncoding(t *testing.T) {
	w := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(w)
	engine.Use(Gzip())
	engine.GET("/x.js", func(c *gin.Context) { c.String(200, "hello") })
	c.Request = httptest.NewRequest(http.MethodGet, "/x.js", nil)
	engine.ServeHTTP(w, c.Request)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("不该压缩，却设置了 Content-Encoding=%q", enc)
	}
	if w.Body.String() != "hello" {
		t.Fatalf("响应被改动: %q", w.Body.String())
	}
}

func TestGzipCompressesWhenAccepted(t *testing.T) {
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(Gzip())
	engine.GET("/x.js", func(c *gin.Context) {
		c.String(200, strings.Repeat("a", 2048))
	})
	req := httptest.NewRequest(http.MethodGet, "/x.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	engine.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("应当压缩")
	}
	// 高度重复的内容压缩后必须明显更小。
	if w.Body.Len() >= 2048 {
		t.Fatalf("压缩无效，大小 %d", w.Body.Len())
	}
}

// WebSocket 握手走 hijack，压缩会破坏它。
func TestGzipSkipsWebSocketUpgrade(t *testing.T) {
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(Gzip())
	engine.GET("/ws_visitor", func(c *gin.Context) { c.String(200, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/ws_visitor", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-Websocket-Key", "x")
	engine.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "" {
		t.Fatalf("WebSocket 升级不该被压缩，得到 %q", enc)
	}
}

// 没有 token 的请求必须被拒，否则任何人凭 visitor_id 就能读聊天记录。
func TestVisitorScopeRejectsMissingToken(t *testing.T) {
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/m", VisitorScope, func(c *gin.Context) { c.String(200, "leaked") })
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/m?visitor_id=v1", nil))

	if strings.Contains(w.Body.String(), "leaked") {
		t.Fatal("无 token 的请求被放行")
	}
}

// 访客 token 只能访问自己的 visitor_id。
func TestVisitorScopeRejectsOtherVisitor(t *testing.T) {
	token, err := tools.MakeToken(map[string]interface{}{"visitor_id": "v1"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/m", VisitorScope, func(c *gin.Context) { c.String(200, "leaked") })
	req := httptest.NewRequest(http.MethodGet, "/m?visitor_id=v2", nil)
	req.Header.Set("token", token)
	engine.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "leaked") {
		t.Fatal("访客读到了别人的会话")
	}
}

func TestVisitorScopeAllowsOwnVisitor(t *testing.T) {
	token, err := tools.MakeToken(map[string]interface{}{"visitor_id": "v1"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/m", VisitorScope, func(c *gin.Context) { c.String(200, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/m?visitor_id=v1", nil)
	req.Header.Set("token", token)
	engine.ServeHTTP(w, req)

	if w.Body.String() != "ok" {
		t.Fatalf("访客应能访问自己的会话，得到 %q", w.Body.String())
	}
}

// 非管理员不能调管理接口。
func TestRbacAuthRejectsNonAdmin(t *testing.T) {
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/admin", func(c *gin.Context) {
		c.Set("kefu_id", float64(2))
		c.Next()
	}, RbacAuth, func(c *gin.Context) { c.String(200, "admin-only") })
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if strings.Contains(w.Body.String(), "admin-only") {
		t.Fatal("非管理员进入了管理接口")
	}
}

func TestRbacAuthAllowsAdmin(t *testing.T) {
	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/admin", func(c *gin.Context) {
		c.Set("kefu_id", float64(1))
		c.Next()
	}, RbacAuth, func(c *gin.Context) { c.String(200, "admin-only") })
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if w.Body.String() != "admin-only" {
		t.Fatalf("管理员应当放行，得到 %q", w.Body.String())
	}
}
