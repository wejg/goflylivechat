package controller

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/models"
	"unicode/utf8"
)

func GetConfigs(c *gin.Context) {
	kefuName, _ := c.Get("kefu_name")
	configs := models.FindConfigsByUserId(kefuName)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": configs,
	})
}
func GetConfig(c *gin.Context) {
	key := c.Query("key")
	config := models.FindConfig(key)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": config,
	})
}
func PostConfig(c *gin.Context) {
	key := c.PostForm("key")
	value, hasValue := c.GetPostForm("value")
	kefuName, _ := c.Get("kefu_name")
	if key == "" || !hasValue || utf8.RuneCountInString(key) > 255 || utf8.RuneCountInString(value) > 255 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "配置键不能为空，配置键和值均不能超过255个字符",
		})
		return
	}
	if err := models.UpdateConfig(kefuName, key, value); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "配置保存失败，请重试"})
		return
	}

	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": "",
	})
}
