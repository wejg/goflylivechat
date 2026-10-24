package controller

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/tools"
	"goflylivechat/ws"
	"mime/multipart"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func messageIdempotencyKey(c *gin.Context) string {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		key = strings.TrimSpace(c.PostForm("client_message_id"))
	}
	if len([]rune(key)) > 100 {
		return ""
	}
	return key
}

func SendMessageV2(c *gin.Context) {
	fromId := c.PostForm("from_id")
	if common.AppChatEnabled() {
		id, _ := c.Get("visitor_id")
		trusted, ok := id.(string)
		if !ok || trusted == "" || (fromId != "" && fromId != trusted) {
			c.JSON(403, gin.H{"code": 403, "msg": "无权使用该访客身份"})
			return
		}
		fromId = trusted
	}
	toId := c.PostForm("to_id")
	content := c.PostForm("content")
	cType := c.PostForm("type")
	clientMessageID := messageIdempotencyKey(c)
	if content == "" || len([]rune(content)) > 2048 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "内容不能为空或超过 2048 个字符",
		})
		return
	}
	if cType == "kefu" {
		c.JSON(200, gin.H{"code": 403, "msg": "客服消息请使用已认证接口"})
		return
	}
	if cType != "visitor" {
		c.JSON(200, gin.H{"code": 400, "msg": "消息类型无效"})
		return
	}
	var kefuInfo models.User
	var vistorInfo models.Visitor
	vistorInfo = models.FindVisitorByVistorId(fromId)
	kefuInfo = models.FindUser(toId)

	if kefuInfo.ID == 0 || vistorInfo.ID == 0 || vistorInfo.ToId != kefuInfo.Name {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "用户不存在",
		})
		return
	}

	if err := checkMessageUploads(content, vistorInfo.VisitorId); err != nil {
		c.JSON(403, gin.H{"code": 403, "msg": err.Error()})
		return
	}
	if existing, found, err := models.FindMessageByClientID(clientMessageID); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "消息查询失败，请重试"})
		return
	} else if found {
		if existing.KefuId != kefuInfo.Name || existing.VisitorId != vistorInfo.VisitorId || existing.MesType != cType {
			c.JSON(200, gin.H{"code": 409, "msg": "消息幂等键已被其他会话使用"})
			return
		}
		c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": gin.H{"message_id": existing.ID}})
		return
	}
	//限流
	if !tools.LimitFreqSingle("sendmessage:"+c.ClientIP(), 1, 2) {
		c.JSON(200, gin.H{"code": 400, "msg": c.ClientIP() + "发送频率过快"})
		return
	}
	messageID, err := models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, content, cType, clientMessageID)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "消息保存失败，请重试"})
		return
	}
	//var msg TypeMessage
	if cType == "visitor" {
		guest, ok := ws.ClientList.Get(vistorInfo.VisitorId)
		if ok && guest != nil {
			guest.Touch()
		}
		//kefuConns, ok := ws.KefuList[kefuInfo.Name]
		//if kefuConns == nil || !ok {
		//	c.JSON(200, gin.H{
		//		"code": 200,
		//		"msg":  "ok",
		//	})
		//	return
		//}
		msg := ws.TypeMessage{
			Type: "message",
			Data: ws.ClientMessage{
				MessageId: messageID,
				Avator:    vistorInfo.Avator,
				Id:        vistorInfo.VisitorId,
				Name:      vistorInfo.Name,
				ToId:      kefuInfo.Name,
				Content:   content,
				Time:      time.Now().Format("2006-01-02 15:04:05"),
				IsKefu:    "no",
			},
		}
		str, _ := json.Marshal(msg)
		ws.OneKefuMessage(kefuInfo.Name, str)
		//ws.KefuMessage(vistorInfo.VisitorId, content, kefuInfo)
		kefu, ok := ws.KefuList.Get(kefuInfo.Name)
		if !ok || kefu == nil {
			go SendNoticeEmail(kefuInfo.Name, content+"|"+vistorInfo.Name, content)
		}
		go ws.VisitorAutoReply(vistorInfo, kefuInfo, content, messageID)
		go models.UpdateVisitorLastMessage(vistorInfo.VisitorId, content)
		c.JSON(200, gin.H{
			"code":   200,
			"msg":    "ok",
			"result": gin.H{"message_id": messageID},
		})
	}

}

func SendKefuMessage(c *gin.Context) {
	fromId, _ := c.Get("kefu_name")
	toId := c.PostForm("to_id")
	content := c.PostForm("content")
	clientMessageID := messageIdempotencyKey(c)
	if content == "" || len([]rune(content)) > 2048 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "内容不能为空或超过 2048 个字符",
		})
		return
	}
	var kefuInfo models.User
	var vistorInfo models.Visitor
	kefuInfo = models.FindUser(fromId.(string))
	vistorInfo = models.FindVisitorByVistorId(toId)

	if kefuInfo.ID == 0 || vistorInfo.ID == 0 || (common.AppChatEnabled() && vistorInfo.ToId != kefuInfo.Name) {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "用户不存在",
		})
		return
	}

	if err := checkMessageUploads(content, vistorInfo.VisitorId); err != nil {
		c.JSON(403, gin.H{"code": 403, "msg": err.Error()})
		return
	}
	if existing, found, err := models.FindMessageByClientID(clientMessageID); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "消息查询失败，请重试"})
		return
	} else if found {
		if existing.KefuId != kefuInfo.Name || existing.VisitorId != vistorInfo.VisitorId || existing.MesType != "kefu" {
			c.JSON(200, gin.H{"code": 409, "msg": "消息幂等键已被其他会话使用"})
			return
		}
		c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": gin.H{"message_id": existing.ID}})
		return
	}
	if !tools.LimitFreqSingle("sendmessage:"+c.ClientIP(), 1, 2) {
		c.JSON(200, gin.H{"code": 400, "msg": c.ClientIP() + "发送频率过快"})
		return
	}
	messageID, err := models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, content, "kefu", clientMessageID)
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "消息保存失败，请重试"})
		return
	}
	//var msg TypeMessage

	guest, ok := ws.ClientList.Get(vistorInfo.VisitorId)

	if guest != nil && ok {
		ws.VisitorMessage(vistorInfo.VisitorId, content, kefuInfo, messageID)
	}
	ws.KefuMessage(vistorInfo.VisitorId, content, kefuInfo, messageID)
	go models.UpdateVisitorLastMessage(vistorInfo.VisitorId, content)
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": gin.H{"message_id": messageID},
	})

}
func SendVisitorNotice(c *gin.Context) {
	notice := c.Query("msg")
	if notice == "" {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "msg不能为空",
		})
		return
	}
	// 只广播给调用者自己的访客，避免一个客服向全站访客推送。
	kefuName, _ := c.Get("kefu_name")
	msg := ws.TypeMessage{
		Type: "notice",
		Data: notice,
	}
	for _, visitor := range ws.ClientList.Snapshot() {
		if visitor.RouteTo() != kefuName {
			continue
		}
		_ = ws.WriteJSON(visitor, msg)
	}
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
	})
}
func SendCloseMessageV2(c *gin.Context) {
	visitorId := c.Query("visitor_id")
	if visitorId == "" {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "visitor_id不能为空",
		})
		return
	}

	kefuName, _ := c.Get("kefu_name")
	oldUser, ok := ws.ClientList.Get(visitorId)
	if oldUser != nil && ok {
		// 只能关闭分配给自己的访客会话。
		if oldUser.RouteTo() != kefuName {
			c.JSON(200, gin.H{"code": 403, "msg": "无权操作该访客"})
			return
		}
		err := ws.WriteJSON(oldUser, ws.TypeMessage{
			Type: "force_close",
			Data: visitorId,
		})
		oldUser.Conn.Close()
		ws.ClientList.Delete(visitorId)
		tools.Logger().Println("close_message", visitorId, err)
	}
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
	})
}
func UploadImg(c *gin.Context) {
	f, err := c.FormFile("imgfile")
	if err != nil {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "上传失败!",
		})
		return
	}
	filepath, _, ok := saveUpload(c, f, allowedImageExts, maxImageSize, "上传失败!图片不允许超过 10M")
	if !ok {
		return
	}
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "上传成功!",
		"result": gin.H{
			"path": filepath,
		},
	})
}

// 允许上传的附件类型。白名单而非黑名单：
// 上传目录通过 /static 直接对外，一旦能传 .html/.svg 就等于在本站域名下托管脚本。
var allowedFileExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".pdf": true, ".txt": true, ".csv": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".zip": true, ".rar": true, ".7z": true,
	".mp3": true, ".mp4": true,
}

var allowedImageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
}

const (
	maxImageSize = 10 * 1024 * 1024
	maxFileSize  = 20 * 1024 * 1024
)

// saveUpload 承载两个上传接口的公共逻辑：类型校验、大小校验、落盘。
func saveUpload(c *gin.Context, f *multipart.FileHeader, allowed map[string]bool, maxSize int64, tip string) (string, string, bool) {
	fileExt := strings.ToLower(path.Ext(f.Filename))
	if f.Size > maxSize {
		c.JSON(200, gin.H{"code": 400, "msg": tip})
		return "", "", false
	}
	if !allowed[fileExt] {
		c.JSON(200, gin.H{"code": 400, "msg": "上传失败!不支持的文件类型"})
		return "", "", false
	}
	fildDir := fmt.Sprintf("%s%d%s/", common.Upload, time.Now().Year(), time.Now().Month().String())
	if err := os.MkdirAll(fildDir, 0o755); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "上传失败!"})
		return "", "", false
	}
	fileName := tools.Md5(fmt.Sprintf("%s%s", f.Filename, time.Now().String()))
	filepath := fmt.Sprintf("%s%s%s", fildDir, fileName, fileExt)
	if err := c.SaveUploadedFile(f, filepath); err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "上传失败!"})
		return "", "", false
	}
	publicPath, ok := privateUploadPath(c, filepath, f.Filename)
	if !ok {
		_ = os.Remove(filepath)
		return "", "", false
	}
	return publicPath, fileExt, true
}

func UploadFile(c *gin.Context) {
	f, err := c.FormFile("realfile")
	if err != nil {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "上传失败!",
		})
		return
	}
	filepath, fileExt, ok := saveUpload(c, f, allowedFileExts, maxFileSize, "上传失败!不允许超过20M")
	if !ok {
		return
	}
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "上传成功!",
		"result": gin.H{
			"path": filepath,
			"ext":  fileExt,
			"size": f.Size,
			"name": f.Filename,
		},
	})
}
func GetMessagespages(c *gin.Context) {
	visitorId := c.Query("visitor_id")
	if !visitorScopeAllows(c, visitorId) {
		c.JSON(200, gin.H{"code": 403, "msg": "无权访问该会话"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pagesize", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 20 {
		pageSize = 20
	}
	queryPage := page
	query := "message.visitor_id = ?"
	args := []interface{}{visitorId}
	_, before := c.Request.URL.Query()["before_id"]
	_, after := c.Request.URL.Query()["after_id"]
	if before && after {
		c.JSON(200, gin.H{"code": 400, "msg": "不能同时指定前后游标"})
		return
	}
	if before || after {
		key, operator := "before_id", " < "
		if after {
			key, operator = "after_id", " > "
		}
		id, err := strconv.ParseUint(c.Query(key), 10, 32)
		if err != nil || id == 0 {
			c.JSON(200, gin.H{"code": 400, "msg": key + "必须是正整数消息ID"})
			return
		}
		query += " AND message.id" + operator + "?"
		args = append(args, id)
		queryPage = 1
	}
	count := models.CountMessage("visitor_id = ?", visitorId)
	list := models.FindMessageByPage(uint(queryPage), uint(pageSize), after, query, args...)
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
		"result": gin.H{
			"count":    count,
			"page":     page,
			"list":     list,
			"pagesize": pageSize,
		},
	})
}
