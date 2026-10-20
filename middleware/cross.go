package middleware

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"net/http"
)

func CrossSite(c *gin.Context) {
	origin := c.Request.Header.Get("Origin")
	if origin != "" {
		if !common.IsAllowedOrigin(origin) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Vary", "Origin")
		c.Header("Access-Control-Allow-Credentials", "true")
	}
	//服务器支持的所有跨域请求的方法
	c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE,UPDATE")
	//允许跨域设置可以返回其他子段，可以自定义字段
	c.Header("Access-Control-Allow-Headers", "Authorization, Content-Length, X-CSRF-Token, Token,session")
	// 允许浏览器（客户端）可以解析的头部 （重要）
	c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
	c.Next()
}
