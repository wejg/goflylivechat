package middleware

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
)

// SessionHandler 创建并返回会话中间件
func SessionHandler() gin.HandlerFunc {
	store := SessionConfig()
	return sessions.Sessions("GOFLY", store)
}

// SessionConfig 配置会话存储
func SessionConfig() sessions.Store {
	sessionMaxAge := 3600
	sessionSecret := os.Getenv("GOFLY_SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "GOFLY"
	}

	store := cookie.NewStore([]byte(sessionSecret))
	store.Options(sessions.Options{
		MaxAge:   sessionMaxAge, // seconds
		Path:     "/",
		HttpOnly: true,                 // 建议添加，防止XSS攻击
		Secure:   true,                 // 生产环境建议启用，要求HTTPS
		SameSite: http.SameSiteLaxMode, // 防止CSRF攻击
	})
	return store
}
