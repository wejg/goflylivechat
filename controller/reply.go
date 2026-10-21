package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"goflylivechat/models"
	"log"
	"unicode/utf8"
)

type ReplyForm struct {
	GroupName string `form:"group_name" binding:"required,max=50"`
}
type ReplyContentForm struct {
	GroupId  string `form:"group_id" binding:"required"`
	Content  string `form:"content" binding:"required,max=1024"`
	ItemName string `form:"item_name" binding:"required,max=50"`
}

func GetReplys(c *gin.Context) {
	kefuId, _ := c.Get("kefu_name")
	log.Println(kefuId)
	res := models.FindReplyByUserId(kefuId)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": res,
	})
}
func GetAutoReplys(c *gin.Context) {
	kefu_id := c.Query("kefu_id")
	res := models.FindReplyTitleByUserId(kefu_id)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": res,
	})
}
func PostReply(c *gin.Context) {
	var replyForm ReplyForm
	kefuId, _ := c.Get("kefu_name")
	err := c.ShouldBind(&replyForm)
	if err != nil {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "error:" + err.Error(),
		})
		return
	}
	replyWriteResponse(c, models.CreateReplyGroup(replyForm.GroupName, kefuId.(string)))
}
func PostReplyContent(c *gin.Context) {
	var replyContentForm ReplyContentForm
	kefuId, _ := c.Get("kefu_name")
	err := c.ShouldBind(&replyContentForm)
	if err != nil {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "error:" + err.Error(),
		})
		return
	}
	replyWriteResponse(c, models.CreateReplyContent(replyContentForm.GroupId, kefuId.(string), replyContentForm.Content, replyContentForm.ItemName))
}
func PostReplyContentSave(c *gin.Context) {
	kefuId, _ := c.Get("kefu_name")
	replyId := c.PostForm("reply_id")
	replyTitle := c.PostForm("reply_title")
	replyContent := c.PostForm("reply_content")
	if replyId == "" || replyTitle == "" || replyContent == "" || utf8.RuneCountInString(replyTitle) > 50 || utf8.RuneCountInString(replyContent) > 1024 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "标题和内容不能为空，标题最多50个字符，内容最多1024个字符",
		})
		return
	}
	replyWriteResponse(c, models.UpdateReplyContent(replyId, kefuId.(string), replyTitle, replyContent))
}
func DelReplyContent(c *gin.Context) {
	kefuId, _ := c.Get("kefu_name")
	id := c.Query("id")
	replyWriteResponse(c, models.DeleteReplyContent(id, kefuId.(string)))
}
func DelReplyGroup(c *gin.Context) {
	kefuId, _ := c.Get("kefu_name")
	id := c.Query("id")
	replyWriteResponse(c, models.DeleteReplyGroup(id, kefuId.(string)))
}
func PostReplySearch(c *gin.Context) {
	kefuId, _ := c.Get("kefu_name")
	search := c.PostForm("search")
	if search == "" {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "参数错误",
		})
		return
	}
	res := models.FindReplyBySearcch(kefuId, search)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": res,
	})
}

func replyWriteResponse(c *gin.Context, err error) {
	if err != nil {
		code, message := 500, "保存失败，请重试"
		if gorm.IsRecordNotFoundError(err) {
			code, message = 400, "分组或回复不存在，请刷新后重试"
		}
		c.JSON(200, gin.H{"code": code, "msg": message})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "ok"})
}
