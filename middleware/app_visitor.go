package middleware

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/tools"
	"net/http"
	"time"
)

func appAuthFailure(c *gin.Context, code int) {
	c.JSON(code, gin.H{"code": code, "msg": "客服会话无效或已过期，请重新进入"})
	c.Abort()
}
func appVisitorIdentity(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && !common.AppSameOrigin(c.Request) {
		appAuthFailure(c, 403)
		return false
	}
	s, err := models.CurrentAppVisitor(c.Request)
	if err != nil {
		appAuthFailure(c, 401)
		return false
	}
	// Check every alias/value: handlers historically read different parameter names.
	for _, key := range []string{"visitor_id", "visitorId"} {
		requested := append(c.QueryArray(key), c.PostFormArray(key)...)
		for _, id := range requested {
			if id != "" && id != s.VisitorID {
				appAuthFailure(c, 403)
				return false
			}
		}
	}
	c.Set("visitor_id", s.VisitorID)
	c.Set("app_session_hash", s.Hash)
	return true
}

// AppVisitor is for visitor-only routes. Legacy behavior is kept only in disabled mode.
func AppVisitor(c *gin.Context) {
	if !common.AppChatEnabled() {
		c.Next()
		return
	}
	if appVisitorIdentity(c) {
		c.Next()
	}
}

// ChatActor also permits genuine agent JWTs for shared resources.
func ChatActor(c *gin.Context) {
	if !common.AppChatEnabled() {
		c.Next()
		return
	}
	// Explicit agent requests win; embedded visitor uploads must not inherit an admin cookie.
	if c.GetHeader("token") != "" && agentIdentity(c) {
		c.Next()
		return
	}
	if _, err := c.Request.Cookie(common.AppVisitorCookie); err == nil {
		if appVisitorIdentity(c) {
			c.Next()
		}
		return
	}
	if agentIdentity(c) {
		c.Next()
		return
	}
	if appVisitorIdentity(c) {
		c.Next()
	}
}

// AttachmentActor permits the union of independently proven identities for read-only assets.
func AttachmentActor(c *gin.Context) {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.AbortWithStatus(http.StatusMethodNotAllowed)
		return
	}
	if !common.AppChatEnabled() {
		c.Next()
		return
	}
	visitorOK := false
	if session, err := models.CurrentAppVisitor(c.Request); err == nil {
		c.Set("visitor_id", session.VisitorID)
		c.Set("app_session_hash", session.Hash)
		visitorOK = true
	}
	agentOK := agentIdentity(c)
	if !visitorOK && !agentOK {
		appAuthFailure(c, 401)
		return
	}
	c.Next()
}

func agentIdentity(c *gin.Context) bool {
	token := c.GetHeader("token")
	if token == "" {
		if cookie, err := c.Request.Cookie("gofly_agent"); err == nil {
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && !common.AppSameOrigin(c.Request) {
				return false
			}
			token = cookie.Value
		}
	}
	claims := tools.ParseToken(token)
	if claims == nil {
		return false
	}
	created, valid := claims["create_time"].(float64)
	if !valid || created > float64(time.Now().Unix()+60) || float64(time.Now().Unix())-created >= 86400 {
		return false
	}
	name, ok := claims["kefu_name"].(string)
	if !ok || name == "" {
		return false
	}
	if models.DB == nil || models.FindUser(name).ID == 0 {
		return false
	}
	c.Set("kefu_name", name)
	c.Set("kefu_id", claims["kefu_id"])
	return true
}
