package ws

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"log"
	"time"
)

func NewVisitorServer(c *gin.Context) {
	visitorInfo := models.FindVisitorByVistorId(c.Query("visitor_id"))
	if visitorInfo.VisitorId == "" {
		c.JSON(200, gin.H{"code": 400, "msg": "访客不存在"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
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
	go models.UpdateVisitorStatus(visitorInfo.VisitorId, 1)
	//go SendServerJiang(visitorInfo.Name, "来了", c.Request.Host)

	AddVisitorToList(user)

	for {
		//接受消息
		var receive []byte
		_, receive, err := conn.ReadMessage()
		if err != nil {
			if current, ok := ClientList.Get(user.Id); ok && current == user {
				log.Println("删除用户", user.Id)
				ClientList.Delete(user.Id)
				VisitorOffline(user.RouteTo(), user.Id, user.Name)
			}
			log.Println(err)
			return
		}

		_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if !enqueueMessage(&Message{user: user, content: receive}) {
			_ = conn.Close()
			if current, ok := ClientList.Get(user.Id); ok && current == user {
				ClientList.Delete(user.Id)
				VisitorOffline(user.RouteTo(), user.Id, user.Name)
			}
			return
		}
	}
}
func AddVisitorToList(user *User) {
	//用户id对应的连接
	oldUser, ok := ClientList.Get(user.Id)
	if ok && oldUser != nil {
		if err := WriteJSON(oldUser, TypeMessage{Type: "close", Data: user.Id}); err != nil {
			oldUser.Conn.Close()
			user.UpdateTime = oldUser.LastUpdate()
			ClientList.Delete(user.Id)
		}
	}
	ClientList.Set(user.Id, user)
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
func VisitorAutoReply(vistorInfo models.Visitor, kefuInfo models.User, content string) {
	kefu, ok := KefuList.Get(kefuInfo.Name)
	reply := models.FindReplyItemByUserIdTitle(kefuInfo.Name, content)
	if reply.Content != "" {
		time.Sleep(1 * time.Second)
		messageID, err := models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, reply.Content, "kefu")
		if err != nil {
			return
		}
		VisitorMessage(vistorInfo.VisitorId, reply.Content, kefuInfo, messageID)
		KefuMessage(vistorInfo.VisitorId, reply.Content, kefuInfo, messageID)
	}
	if !ok || kefu == nil {
		time.Sleep(1 * time.Second)
		config := models.FindConfigByUserId(kefuInfo.Name, "OfflineMessage")
		if config.ConfValue == "" || reply.Content != "" {
			return
		}
		messageID, err := models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, config.ConfValue, "kefu")
		if err != nil {
			return
		}
		VisitorMessage(vistorInfo.VisitorId, config.ConfValue, kefuInfo, messageID)
	}
}
func CleanVisitorExpire() {
	go func() {
		log.Println("cleanVisitorExpire start...")
		// 超时阈值是 VisitorExpire（默认 600 秒），30 秒一次足够，
		// 不必每 5 秒就 Snapshot 一次全量连接。
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			expired := make([]string, 0)
			for _, user := range ClientList.Snapshot() {
				if time.Since(user.LastUpdate()).Seconds() < common.VisitorExpire {
					continue
				}
				_ = WriteJSON(user, TypeMessage{Type: "auto_close", Data: user.Id})
				_ = user.Conn.Close()
				ClientList.Delete(user.Id)
				expired = append(expired, user.Id)
			}
			models.UpdateVisitorsOffline(expired)
		}
	}()
}
