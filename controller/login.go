package controller

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/models"
	"goflylivechat/tools"
	"time"
)

// @Summary User Authentication API
// @Description Validates user credentials and returns access token
// @Tags Authentication
// @Produce json
// @Accept multipart/form-data
// @Param username formData string true "Registered username"
// @Param password formData string true "Account password"
// @Param type formData string true "Auth type (e.g., 'admin' or 'user')"
// @Success 200 {object} Response
// @Failure 401 {object} Response
// @Failure 500 {object} Response
// @Router /check [post]
func LoginCheckPass(c *gin.Context) {
	if !tools.LimitFreqSingle("login:"+c.ClientIP(), 5, 60) {
		c.JSON(200, gin.H{"code": 429, "message": "登录尝试过于频繁"})
		return
	}
	password := c.PostForm("password")
	username := c.PostForm("username")
	info := models.FindUser(username)

	// Authentication failed case
	matched, needsUpgrade := tools.CheckPassword(info.Password, password)
	if info.Name == "" || !matched {
		c.JSON(200, gin.H{
			"code":    401,
			"message": "Incorrect username or password", // User-friendly message
		})
		return
	}

	// 旧的 MD5 记录登录成功后就地升级成 bcrypt，用户无感知。
	if needsUpgrade {
		if hashed, err := tools.HashPassword(password); err == nil {
			models.UpdateUserPass(info.Name, hashed)
		}
	}

	// Prepare user session data
	userinfo := map[string]interface{}{
		"kefu_name":   info.Name,
		"kefu_id":     info.ID,
		"create_time": time.Now().Unix(),
	}

	// Token generation
	token, err := tools.MakeToken(userinfo)
	if err != nil {
		c.JSON(200, gin.H{
			"code":    500,
			"message": "Login temporarily unavailable",
		})
		return
	}

	// Successful response
	c.JSON(200, gin.H{
		"code":    200,
		"message": "Login successful",
		"result": gin.H{
			"token":      token,
			"created_at": userinfo["create_time"],
		},
	})
}
