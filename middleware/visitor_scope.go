package middleware

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/tools"
)

// VisitorScope 保护按 visitor_id 取数据的接口。
//
// 放行两种调用者：
//  1. 持有效客服 JWT 的客服（可查自己负责的访客，由 handler 再校验归属）；
//  2. 持该 visitor_id 对应访客 token 的访客本人。
//
// visitor_id 会出现在 widget URL、Referer 和前端 localStorage 里，
// 泄露路径很多，不能当成鉴权凭据，所以必须带 token。
func VisitorScope(c *gin.Context) {
	if common.AppChatEnabled() {
		ChatActor(c)
		return
	}
	visitorID := requestVisitorID(c)
	if visitorID == "" {
		c.JSON(200, gin.H{"code": 400, "msg": "visitor_id不能为空"})
		c.Abort()
		return
	}

	token := c.GetHeader("token")
	if token == "" {
		token = c.Query("token")
	}
	claims := tools.ParseToken(token)
	if claims == nil {
		c.JSON(200, gin.H{"code": 401, "msg": "验证失败"})
		c.Abort()
		return
	}

	// 客服 token：放行，归属校验交给 handler。
	if name, ok := claims["kefu_name"].(string); ok && name != "" {
		c.Set("kefu_name", name)
		c.Set("kefu_id", claims["kefu_id"])
		c.Next()
		return
	}

	// 访客 token：只能访问自己的 visitor_id。
	if id, ok := claims["visitor_id"].(string); ok && id == visitorID {
		c.Set("visitor_id", id)
		c.Next()
		return
	}

	c.JSON(200, gin.H{"code": 403, "msg": "无权访问该会话"})
	c.Abort()
}

// requestVisitorID 兼容历史上并存的三种参数写法。
func requestVisitorID(c *gin.Context) string {
	if id := c.Query("visitor_id"); id != "" {
		return id
	}
	if id := c.Query("visitorId"); id != "" {
		return id
	}
	return c.PostForm("visitor_id")
}
