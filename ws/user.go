package ws

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"goflylivechat/models"
	"goflylivechat/tools"
	"log"
	"time"
)

func NewKefuServer(c *gin.Context) {
	kefuName, _ := c.Get("kefu_name")
	kefuInfo := models.FindUser(kefuName.(string))
	if kefuInfo.ID == 0 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "用户不存在",
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	conn.SetReadLimit(wsReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	defer func() {
		if current, ok := KefuList.Get(kefuInfo.Name); ok && current != nil && current.Conn == conn {
			KefuList.Delete(kefuInfo.Name)
		}
		_ = conn.Close()
	}()

	var kefu User
	kefu.Id = kefuInfo.Name
	kefu.Name = kefuInfo.Nickname
	kefu.Avator = kefuInfo.Avator
	kefu.Conn = conn
	AddKefuToList(&kefu)

	for {
		//接受消息
		var receive []byte
		messageType, receive, err := conn.ReadMessage()
		if err != nil {
			log.Println("ws/user.go ", err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if !enqueueMessage(&Message{conn: conn, content: receive, context: c, messageType: messageType}) {
			return
		}
	}
}
func AddKefuToList(kefu *User) {
	oldUser, ok := KefuList.Get(kefu.Id)
	if ok && oldUser != nil {
		msg := TypeMessage{
			Type: "close",
			Data: kefu.Id,
		}
		str, _ := json.Marshal(msg)
		oldUser.Mux.Lock()
		err := oldUser.Conn.WriteMessage(websocket.TextMessage, str)
		oldUser.Mux.Unlock()
		if err != nil {
			oldUser.Conn.Close()
		}
	}
	KefuList.Set(kefu.Id, kefu)
}

// 给指定客服发消息
func OneKefuMessage(toId string, str []byte) {
	kefu, ok := KefuList.Get(toId)
	if ok {
		kefu.Mux.Lock()
		defer kefu.Mux.Unlock()
		_ = kefu.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := kefu.Conn.WriteMessage(websocket.TextMessage, str); err != nil {
			KefuList.Delete(toId)
			tools.Logger().Println("send_kefu_message", err)
		}
	}
}
func KefuMessage(visitorId, content string, kefuInfo models.User) {
	msg := TypeMessage{
		Type: "message",
		Data: ClientMessage{
			Name:    kefuInfo.Nickname,
			Avator:  kefuInfo.Avator,
			Id:      visitorId,
			Time:    time.Now().Format("2006-01-02 15:04:05"),
			ToId:    visitorId,
			Content: content,
			IsKefu:  "yes",
		},
	}
	str, _ := json.Marshal(msg)
	OneKefuMessage(kefuInfo.Name, str)
}

// 给客服客户端发送消息判断客户端是否在线
func SendPingToKefuClient() {
	msg := TypeMessage{
		Type: "many pong",
	}
	str, _ := json.Marshal(msg)
	for _, kefu := range KefuList.Snapshot() {
		if kefu == nil {
			continue
		}
		kefu.Mux.Lock()
		err := kefu.Conn.WriteMessage(websocket.TextMessage, str)
		kefu.Mux.Unlock()
		if err != nil {
			log.Println("定时发送ping给客服，失败", err.Error())
			KefuList.Delete(kefu.Id)
		}
	}
}
