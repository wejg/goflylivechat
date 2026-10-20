package ws

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"goflylivechat/common"
	"goflylivechat/models"
	"log"
	"time"
)

func NewVisitorServer(c *gin.Context) {
	//go kefuServerBackend()
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	//获取GET参数,创建WS
	vistorInfo := models.FindVisitorByVistorId(c.Query("visitor_id"))
	if vistorInfo.VisitorId == "" {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "访客不存在",
		})
		return
	}
	user := &User{
		Conn:       conn,
		Name:       vistorInfo.Name,
		Avator:     vistorInfo.Avator,
		Id:         vistorInfo.VisitorId,
		To_id:      vistorInfo.ToId,
		UpdateTime: time.Now(),
	}
	go models.UpdateVisitorStatus(vistorInfo.VisitorId, 1)
	//go SendServerJiang(vistorInfo.Name, "来了", c.Request.Host)

	AddVisitorToList(user)

	for {
		//接受消息
		var receive []byte
		messageType, receive, err := conn.ReadMessage()
		if err != nil {
			if current, ok := ClientList.Get(user.Id); ok && current == user {
				log.Println("删除用户", user.Id)
				ClientList.Delete(user.Id)
				VisitorOffline(user.RouteTo(), user.Id, user.Name)
			}
			log.Println(err)
			return
		}

		message <- &Message{
			conn:        conn,
			content:     receive,
			context:     c,
			messageType: messageType,
		}
	}
}
func AddVisitorToList(user *User) {
	//用户id对应的连接
	oldUser, ok := ClientList.Get(user.Id)
	if ok && oldUser != nil {
		msg := TypeMessage{
			Type: "close",
			Data: user.Id,
		}
		str, _ := json.Marshal(msg)
		oldUser.Mux.Lock()
		err := oldUser.Conn.WriteMessage(websocket.TextMessage, str)
		oldUser.Mux.Unlock()
		if err != nil {
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
	msg := TypeMessage{
		Type: "notice",
		Data: notice,
	}
	str, _ := json.Marshal(msg)
	visitor, ok := ClientList.Get(visitorId)
	if !ok || visitor == nil || visitor.Conn == nil {
		return
	}
	visitor.Mux.Lock()
	visitor.Conn.WriteMessage(websocket.TextMessage, str)
	visitor.Mux.Unlock()
}
func VisitorMessage(visitorId, content string, kefuInfo models.User) {
	msg := TypeMessage{
		Type: "message",
		Data: ClientMessage{
			Name:    kefuInfo.Nickname,
			Avator:  kefuInfo.Avator,
			Id:      kefuInfo.Name,
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			ToId:    visitorId,
			Content: content,
			IsKefu:  "no",
		},
	}
	str, _ := json.Marshal(msg)
	visitor, ok := ClientList.Get(visitorId)
	if !ok || visitor == nil || visitor.Conn == nil {
		return
	}
	visitor.Mux.Lock()
	visitor.Conn.WriteMessage(websocket.TextMessage, str)
	visitor.Mux.Unlock()
}
func VisitorAutoReply(vistorInfo models.Visitor, kefuInfo models.User, content string) {
	kefu, ok := KefuList.Get(kefuInfo.Name)
	reply := models.FindReplyItemByUserIdTitle(kefuInfo.Name, content)
	if reply.Content != "" {
		time.Sleep(1 * time.Second)
		VisitorMessage(vistorInfo.VisitorId, reply.Content, kefuInfo)
		KefuMessage(vistorInfo.VisitorId, reply.Content, kefuInfo)
		models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, reply.Content, "kefu")
	}
	if !ok || kefu == nil {
		time.Sleep(1 * time.Second)
		config := models.FindConfigByUserId(kefuInfo.Name, "OfflineMessage")
		if config.ConfValue == "" || reply.Content != "" {
			return
		}
		VisitorMessage(vistorInfo.VisitorId, config.ConfValue, kefuInfo)
		models.CreateMessage(kefuInfo.Name, vistorInfo.VisitorId, config.ConfValue, "kefu")
	}
}
func CleanVisitorExpire() {
	go func() {
		log.Println("cleanVisitorExpire start...")
		for {
			for _, user := range ClientList.Snapshot() {
				diff := time.Since(user.LastUpdate()).Seconds()
				if diff >= common.VisitorExpire {
					msg := TypeMessage{
						Type: "auto_close",
						Data: user.Id,
					}
					str, _ := json.Marshal(msg)
					user.Mux.Lock()
					err := user.Conn.WriteMessage(websocket.TextMessage, str)
					user.Mux.Unlock()
					if err != nil {
						user.Conn.Close()
						ClientList.Delete(user.Id)
					}
					log.Println(user.Name + ":cleanVisitorExpire finshed")
				}
			}
			t := time.NewTimer(time.Second * 5)
			<-t.C
		}
	}()
}
