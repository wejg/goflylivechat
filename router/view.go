package router

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"net/http"
)

func InitViewRouter(engine *gin.Engine) {
	//engine.GET("/", tmpl.PageIndex)
	engine.GET("/login", PageLogin)
	engine.GET("/pannel", PagePannel)
	engine.GET("/livechat", PageChat)
	engine.GET("/main", PageMain)
	engine.GET("/chat_main", PageChatMain)
	engine.GET("/setting", PageSetting)
}

// Login page
func PageLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", nil)
}

// Dashboard
func PagePannel(c *gin.Context) {
	c.HTML(http.StatusOK, "pannel.html", nil)
}

// Admin console
func PageMain(c *gin.Context) {
	c.HTML(http.StatusOK, "main.html", nil)
}

// Customer chat interface
func PageChat(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	referralSource := c.Query("refer") // More clear variable name

	if referralSource == "" {
		referralSource = c.Request.Referer()
	}
	if referralSource == "" {
		referralSource = "Direct access" // More natural English
	}

	c.HTML(http.StatusOK, "chat_page.html", gin.H{
		"AppChatEnabled": common.AppChatEnabled(),
		"Refer":          referralSource, // Keeping original template variable name
	})
}

// Support agent console
func PageChatMain(c *gin.Context) {
	c.HTML(http.StatusOK, "chat_main.html", nil)
}

// Settings
func PageSetting(c *gin.Context) {
	c.HTML(http.StatusOK, "setting.html", nil)
}
