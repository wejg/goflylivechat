package middleware

import (
	"github.com/gin-gonic/gin"
)

// adminUserID 是安装时创建的第一个账号，视为管理员。
//
// 这里没有实现完整 RBAC：role / user_role 两张表原本连建表语句都没有，
// 而 RbacAuth 之前第一行就 return，挂着它的 16 个路由等于完全敞开——
// 任何注册用户都能删其他客服、改全局配置。
// 先用「只有管理员能做管理操作」堵住，比留一个假的权限中间件诚实得多。
const adminUserID = 1

// RbacAuth 限制管理类接口只能由管理员调用。
// 必须跟在 JwtApiMiddleware 之后，依赖它设置的 kefu_id。
func RbacAuth(c *gin.Context) {
	id, ok := c.Get("kefu_id")
	if !ok {
		c.JSON(200, gin.H{"code": 401, "msg": "验证失败"})
		c.Abort()
		return
	}
	// JWT 数字 claim 解出来是 float64。
	switch v := id.(type) {
	case float64:
		if int(v) == adminUserID {
			c.Next()
			return
		}
	case int:
		if v == adminUserID {
			c.Next()
			return
		}
	case uint:
		if int(v) == adminUserID {
			c.Next()
			return
		}
	}
	c.JSON(200, gin.H{"code": 403, "msg": "需要管理员权限"})
	c.Abort()
}
