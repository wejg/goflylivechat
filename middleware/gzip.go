package middleware

import (
	"compress/gzip"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"sync"
)

// 只压缩文本类资源；图片、音频、woff 本身已是压缩格式，再压一遍只费 CPU。
var compressibleExt = map[string]bool{
	".js": true, ".css": true, ".html": true, ".json": true,
	".svg": true, ".txt": true, ".xml": true, ".map": true,
}

var gzipPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
		return w
	},
}

type gzipWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.writer.Write(data)
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	return g.writer.Write([]byte(s))
}

// gin 的 Writer 默认会补 Content-Length，压缩后长度对不上，必须去掉。
func (g *gzipWriter) WriteHeader(code int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}

// Gzip 压缩文本类静态资源。访客页要加载约 1MB 的 JS/CSS，
// 压缩后可降到三分之一左右，这是嵌入式 widget 最直接的优化。
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}
		// WebSocket 走 hijack，压缩会破坏握手。
		if strings.EqualFold(c.GetHeader("Connection"), "upgrade") ||
			c.GetHeader("Sec-Websocket-Key") != "" {
			c.Next()
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/attachments/") || !shouldCompress(c.Request) {
			c.Next()
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(c.Writer)
		defer func() {
			gz.Close()
			gzipPool.Put(gz)
		}()

		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")
		c.Writer = &gzipWriter{c.Writer, gz}
		c.Next()
	}
}

func shouldCompress(r *http.Request) bool {
	path := r.URL.Path
	if idx := strings.LastIndex(path, "."); idx != -1 {
		return compressibleExt[strings.ToLower(path[idx:])]
	}
	// 无扩展名的是 HTML 页面和 JSON 接口，都值得压。
	return true
}
