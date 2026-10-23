package controller

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"net/http"
	"os"
)

const agentCookieName = "gofly_agent"

func setAgentCookie(c *gin.Context, token string, age int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: agentCookieName, Value: token, Path: "/", MaxAge: age, Secure: os.Getenv("GOFLY_ENV") != "development", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	c.Header("Cache-Control", "private, no-store")
}
func SetAgentSession(c *gin.Context) {
	if common.AppChatEnabled() && !common.AppSameOrigin(c.Request) {
		c.AbortWithStatus(403)
		return
	}
	setAgentCookie(c, c.GetHeader("token"), 86400)
	c.JSON(200, gin.H{"code": 200})
}
func DeleteAgentSession(c *gin.Context) {
	if !common.AppSameOrigin(c.Request) {
		c.AbortWithStatus(403)
		return
	}
	setAgentCookie(c, "", -1)
	c.JSON(200, gin.H{"code": 200})
}
