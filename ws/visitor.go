package ws

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"log"
	"sync"
	"time"
)

var visitorConnectionMu sync.Mutex

func CloseAppSession(hash string) {
	for _, user := range ClientList.Snapshot() {
		if user.AppSessionHash == hash && user.Conn != nil {
			_ = user.Conn.Close()
		}
	}
}

func watchAppSession(ctx context.Context, user *User, initial *models.AppVisitorSession) {
	session := initial
	for {
		// Refresh early enough that the five-second source timeout cannot outlive the lease.
		deadline := session.VerifiedUntil
		expireOnly := !session.ExpiresAt.After(deadline)
		if !expireOnly {
			deadline = deadline.Add(-10 * time.Second)
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if expireOnly {
			_ = user.Conn.Close()
			return
		}
		next, err := models.RefreshAppSessionHash(ctx, user.AppSessionHash)
		if err != nil {
			_ = user.Conn.Close()
			return
		}
		session = next
	}
}

func removeVisitorConnection(user *User) {
	visitorConnectionMu.Lock()
	defer visitorConnectionMu.Unlock()
	if current, ok := ClientList.Get(user.Id); ok && current == user {
		ClientList.Delete(user.Id)
		VisitorOffline(user.RouteTo(), user.Id, user.Name)
	}
}

func NewVisitorServer(c *gin.Context) {
	visitorID := c.Query("visitor_id")
	var appSession *models.AppVisitorSession
	if common.AppChatEnabled() {
		if !common.AppSameOrigin(c.Request) {
			c.JSON(403, gin.H{"code": 403, "msg": "来源验证失败"})
			return
		}
		var err error
		appSession, err = models.CurrentAppVisitor(c.Request)
		if err != nil {
			c.JSON(401, gin.H{"code": 401, "msg": "客服会话无效"})
			return
		}
		if visitorID != "" && visitorID != appSession.VisitorID {
			c.JSON(403, gin.H{"code": 403, "msg": "无权访问"})
			return
		}
		visitorID = appSession.VisitorID
	}
	visitorInfo := models.FindVisitorByVistorId(visitorID)
	if visitorInfo.VisitorId == "" {
		c.JSON(200, gin.H{"code": 400, "msg": "访客不存在"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(wsReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	user := &User{
		Conn:       conn,
		Name:       visitorInfo.Name,
		Avator:     visitorInfo.Avator,
		Id:         visitorInfo.VisitorId,
		To_id:      visitorInfo.ToId,
		UpdateTime: time.Now(),
	}
	if appSession != nil {
		user.AppSessionHash = appSession.Hash
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	if appSession != nil {
		go watchAppSession(ctx, user, appSession)
	}
	//go SendServerJiang(visitorInfo.Name, "来了", c.Request.Host)

	AddVisitorToList(user)

	for {
		//接受消息
		var receive []byte
		_, receive, err := conn.ReadMessage()
		if err != nil {
			removeVisitorConnection(user)
			log.Println(err)
			return
		}

		if appSession != nil {
			if _, err := models.ValidateAppSessionHash(ctx, user.AppSessionHash); err != nil {
				removeVisitorConnection(user)
				return
			}
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if !enqueueMessage(&Message{user: user, content: receive}) {
			_ = conn.Close()
			removeVisitorConnection(user)
			return
		}
	}
}
func AddVisitorToList(user *User) {
	visitorConnectionMu.Lock()
	oldUser, ok := ClientList.Get(user.Id)
	ClientList.Set(user.Id, user)
	models.UpdateVisitorStatus(user.Id, 1)
	if ok && oldUser != nil && oldUser != user {
		_ = WriteJSON(oldUser, TypeMessage{Type: "close", Data: user.Id})
		if oldUser.Conn != nil {
			_ = oldUser.Conn.Close()
		}
	}
	visitorConnectionMu.Unlock()
	lastMessage := models.FindLastMessageByVisitorId(user.Id)
	userInfo := make(map[string]string)
	userInfo["uid"] = user.Id
	userInfo["username"] = user.Name
	userInfo["avator"] = user.Avator
	userInfo["last_message"] = lastMessage.Content
	if userInfo["last_message"] == "" {
		userInfo["last_message"] = "new visitor"
	}
	msg := TypeMessage{
		Type: "userOnline",
		Data: userInfo,
	}
	str, _ := json.Marshal(msg)

	//新版
	OneKefuMessage(user.RouteTo(), str)
}
func VisitorOnline(kefuId string, visitor models.Visitor) {
	lastMessage := models.FindLastMessageByVisitorId(visitor.VisitorId)
	userInfo := make(map[string]string)
	userInfo["uid"] = visitor.VisitorId
	userInfo["username"] = visitor.Name
	userInfo["avator"] = visitor.Avator
	userInfo["last_message"] = lastMessage.Content
	if userInfo["last_message"] == "" {
		userInfo["last_message"] = "new visitor"
	}
	msg := TypeMessage{
		Type: "userOnline",
		Data: userInfo,
	}
	str, _ := json.Marshal(msg)
	OneKefuMessage(kefuId, str)
}
func VisitorOffline(kefuId string, visitorId string, visitorName string) {

	models.UpdateVisitorStatus(visitorId, 0)
	VisitorRemoved(kefuId, visitorId, visitorName)
}

func VisitorRemoved(kefuId, visitorId, visitorName string) {
	userInfo := make(map[string]string)
	userInfo["uid"] = visitorId
	userInfo["name"] = visitorName
	msg := TypeMessage{
		Type: "userOffline",
		Data: userInfo,
	}
	str, _ := json.Marshal(msg)
	//新版
	OneKefuMessage(kefuId, str)
}
func VisitorNotice(visitorId string, notice string) {
	visitor, ok := ClientList.Get(visitorId)
	if !ok || visitor == nil {
		return
	}
	_ = WriteJSON(visitor, TypeMessage{Type: "notice", Data: notice})
}
func VisitorMessage(visitorId, content string, kefuInfo models.User, messageID uint) {
	visitor, ok := ClientList.Get(visitorId)
	if !ok || visitor == nil {
		return
	}
	_ = WriteJSON(visitor, TypeMessage{
		Type: "message",
		Data: ClientMessage{
			MessageId: messageID,
			Name:      kefuInfo.Nickname,
			Avator:    kefuInfo.Avator,
			Id:        kefuInfo.Name,
			Time:      time.Now().Format("2006-01-02 15:04:05"),
			ToId:      visitorId,
			Content:   content,
			IsKefu:    "no",
		},
	})
}
func VisitorAutoReply(vistorInfo models.Visitor, kefuInfo models.User, content string, visitorMessageID uint) {
	reply := models.FindReplyItemByUserIdTitle(kefuInfo.Name, content)
	if reply.Content != "" {
		time.Sleep(1 * time.Second)
		messageID, err := models.CreateAutoReply(kefuInfo.Name, vistorInfo.VisitorId, reply.Content, "keyword", visitorMessageID)
		if err != nil || messageID == 0 {
			return
		}
		VisitorMessage(vistorInfo.VisitorId, reply.Content, kefuInfo, messageID)
		KefuMessage(vistorInfo.VisitorId, reply.Content, kefuInfo, messageID)
		return
	}
	kefu, online := KefuList.Get(kefuInfo.Name)
	if online && kefu != nil {
		return
	}
	config := models.FindConfigByUserId(kefuInfo.Name, "OfflineMessage")
	if config.ConfValue == "" {
		return
	}
	time.Sleep(1 * time.Second)
	if kefu, online = KefuList.Get(kefuInfo.Name); online && kefu != nil {
		return
	}
	messageID, err := models.CreateAutoReply(kefuInfo.Name, vistorInfo.VisitorId, config.ConfValue, "offline", visitorMessageID)
	if err != nil || messageID == 0 {
		return
	}
	VisitorMessage(vistorInfo.VisitorId, config.ConfValue, kefuInfo, messageID)
}
func CleanVisitorExpire() {
	go func() {
		log.Println("cleanVisitorExpire start...")
		// 超时阈值是 VisitorExpire（默认 600 秒），30 秒一次足够，
		// 不必每 5 秒就 Snapshot 一次全量连接。
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for _, user := range ClientList.Snapshot() {
				if time.Since(user.LastUpdate()).Seconds() < common.VisitorExpire {
					continue
				}
				_ = WriteJSON(user, TypeMessage{Type: "auto_close", Data: user.Id})
				_ = user.Conn.Close()
				removeVisitorConnection(user)
			}
		}
	}()
}
