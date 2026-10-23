package middleware

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"path"
	"strings"
)

// App chat uploads are served only through /attachments after ownership validation.
func PrivateUploadGuard(c *gin.Context) {
	if common.AppChatEnabled() && (path.Clean(c.Request.URL.Path) == "/static/upload" || strings.HasPrefix(path.Clean(c.Request.URL.Path), "/static/upload/")) {
		c.Header("Cache-Control", "private, no-store")
		c.AbortWithStatus(404)
		return
	}
	c.Next()
}
