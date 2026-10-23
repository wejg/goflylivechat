package controller

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

func GetPrivateUpload(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	visitor, _ := c.Get("visitor_id")
	agent, _ := c.Get("kefu_name")
	allowed, err := models.CanReadPrivateUpload(c.Param("id"), stringActor(visitor), "")
	if err == nil && !allowed && stringActor(agent) != "" {
		allowed, err = models.CanReadPrivateUpload(c.Param("id"), "", stringActor(agent))
	}
	if err != nil {
		c.JSON(503, gin.H{"code": 503, "msg": "附件暂不可用"})
		return
	}
	if !allowed {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	var row models.PrivateUpload
	if err := models.DB.Where("id=?", c.Param("id")).First(&row).Error; err != nil {
		c.AbortWithStatus(404)
		return
	}
	ext := strings.ToLower(filepath.Ext(row.Name))
	disposition := "attachment"
	if allowedImageExts[ext] {
		disposition = "inline"
		c.Header("Content-Type", mime.TypeByExtension(ext))
	} else {
		c.Header("Content-Type", "application/octet-stream")
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": row.Name}))
	c.File(row.Path)
}
func stringActor(v interface{}) string { value, _ := v.(string); return value }

// Message attachment references must belong to this conversation, even when an agent owns both.
var uploadToken = regexp.MustCompile(`img\[[^\]]*\]|attachment\[[\s\S]*?\]`)

func checkMessageUploads(content, visitorID string) error {
	if !common.AppChatEnabled() {
		return nil
	}
	for _, token := range uploadToken.FindAllString(content, -1) {
		reference := strings.TrimSuffix(strings.TrimPrefix(token, "img["), "]")
		if strings.HasPrefix(token, "attachment[") {
			var info struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(token[11:len(token)-1]), &info) != nil {
				continue // Invalid tokens are rendered as plain text by the client.
			}
			reference = info.Path
		}
		// Match browser URL normalization before checking the routed, decoded path.
		reference = strings.NewReplacer("\t", "", "\r", "", "\n", "", "\\", "/").Replace(reference)
		reference = strings.TrimFunc(reference, func(r rune) bool { return r <= 0x20 })
		u, err := url.Parse(reference)
		if err != nil || u.Opaque != "" || ((u.Scheme != "" || strings.HasPrefix(reference, "//")) && u.Host == "") {
			return fmt.Errorf("无效的附件地址")
		}
		decodedPath := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
		if !strings.HasPrefix(decodedPath, "/attachments/") {
			continue
		}
		id := strings.TrimPrefix(decodedPath, "/attachments/")
		var row models.PrivateUpload
		if err := models.DB.Where("id=? AND visitor_id=?", id, visitorID).First(&row).Error; err != nil {
			return fmt.Errorf("附件不属于当前会话")
		}
	}
	return nil
}

func privateUploadPath(c *gin.Context, filePath, name string) (string, bool) {
	if !common.AppChatEnabled() {
		return filePath, true
	}
	v, _ := c.Get("visitor_id")
	a, _ := c.Get("kefu_name")
	visitorID, agentID := stringActor(v), stringActor(a)
	if agentID != "" {
		visitorID = c.PostForm("visitor_id")
		if visitorID != "" && models.FindVisitorByVistorId(visitorID).ToId != agentID {
			c.JSON(403, gin.H{"code": 403, "msg": "无权上传到该会话"})
			return "", false
		}
	}
	row, err := models.RecordPrivateUpload(filePath, name, visitorID, agentID)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "附件保存失败"})
		return "", false
	}
	return "attachments/" + row.ID, true
}
