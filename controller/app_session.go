package controller

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/ws"
)

func appSessionResponse(c *gin.Context, visitorID string) {
	v := models.FindVisitorByVistorId(visitorID)
	if v.ID == 0 {
		c.JSON(401, gin.H{"code": 401, "msg": "会话不存在"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": v})
}
func appSessionEnabled(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if !common.AppChatEnabled() {
		c.JSON(404, gin.H{"code": 404, "msg": "App客服未启用"})
		return false
	}
	return true
}
func setAppCookie(c *gin.Context, token string, expiry time.Time, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: common.AppVisitorCookie, Value: token, Path: "/", HttpOnly: true, Secure: os.Getenv("GOFLY_ENV") != "development", SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: maxAge})
}
func PostAppSession(c *gin.Context) {
	if !appSessionEnabled(c) {
		return
	}
	if !common.AppSameOrigin(c.Request) {
		c.JSON(403, gin.H{"code": 403, "msg": "来源验证失败"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		Ticket string `json:"ticket"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(interface{})) != io.EOF || len(input.Ticket) < 32 || len(input.Ticket) > 256 {
		c.JSON(400, gin.H{"code": 400, "msg": "无效的客服票据"})
		return
	}
	visitor, token, session, err := models.CreateAppVisitorSession(c.Request.Context(), input.Ticket, c.ClientIP())
	if err != nil {
		c.JSON(401, gin.H{"code": 401, "msg": "客服入口已失效，请重新进入"})
		return
	}
	// Rotate this browser's previous cookie only; other devices keep their own session.
	if old, err := c.Request.Cookie(common.AppVisitorCookie); err == nil && len(old.Value) == 43 {
		hash := models.AppSessionHash(old.Value)
		if err := models.RevokeAppSession(hash); err != nil {
			if cleanupErr := models.RevokeAppSession(session.Hash); cleanupErr != nil {
				log.Printf("failed to revoke unpublished replacement chat session: %v", cleanupErr)
			}
			c.JSON(503, gin.H{"code": 503, "msg": "客服会话切换失败，请重试"})
			return
		}
		ws.CloseAppSession(hash)
	}
	setAppCookie(c, token, session.ExpiresAt, int(time.Until(session.ExpiresAt).Seconds()))
	c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": visitor})
}
func GetAppSession(c *gin.Context) {
	if !appSessionEnabled(c) {
		return
	}
	s, err := models.CurrentAppVisitor(c.Request)
	if err != nil {
		c.JSON(401, gin.H{"code": 401, "msg": "请重新进入客服"})
		return
	}
	appSessionResponse(c, s.VisitorID)
}
func DeleteAppSession(c *gin.Context) {
	if !appSessionEnabled(c) {
		return
	}
	if !common.AppSameOrigin(c.Request) {
		c.JSON(403, gin.H{"code": 403, "msg": "来源验证失败"})
		return
	}
	if cookie, err := c.Request.Cookie(common.AppVisitorCookie); err == nil && len(cookie.Value) == 43 {
		hash := models.AppSessionHash(cookie.Value)
		if err = models.RevokeAppSession(hash); err != nil {
			c.JSON(503, gin.H{"code": 503, "msg": "退出失败，请重试"})
			return
		}
		ws.CloseAppSession(hash)
	}
	setAppCookie(c, "", time.Unix(1, 0), -1)
	c.JSON(200, gin.H{"code": 200, "msg": "ok"})
}
