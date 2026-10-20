package middleware

import (
	"github.com/gin-gonic/gin"
	"strings"
)

// StaticCache 给 /static 下的资源加长缓存。
// 这些文件是版本化的第三方库和图片，内容变了文件名或 ?v= 也会变，
// 可以安全地长期缓存，省掉每次访问的条件请求。
func StaticCache(maxAgeSeconds string) gin.HandlerFunc {
	value := "public, max-age=" + maxAgeSeconds
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Header("Cache-Control", value)
		}
		c.Next()
	}
}
