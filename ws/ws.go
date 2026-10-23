package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"goflylivechat/common"
	"goflylivechat/internal/registry"
	"goflylivechat/models"
	"goflylivechat/tools"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type User struct {
	Conn           *websocket.Conn
	Name           string
	Id             string
	Avator         string
	To_id          string
	Role_id        string
	Mux            sync.Mutex
	UpdateTime     time.Time
	AppSessionHash string
}

func (u *User) Touch() {
	u.Mux.Lock()
	u.UpdateTime = time.Now()
	u.Mux.Unlock()
}

func (u *User) LastUpdate() time.Time {
	u.Mux.Lock()
	updated := u.UpdateTime
	u.Mux.Unlock()
	return updated
}

func (u *User) RouteTo() string {
	u.Mux.Lock()
	toID := u.To_id
	u.Mux.Unlock()
	return toID
}

func (u *User) SetRouteTo(toID string) {
	u.Mux.Lock()
	u.To_id = toID
	u.Mux.Unlock()
}

type Message struct {
	user    *User
	content []byte
}
type TypeMessage struct {
	Type interface{} `json:"type"`
	Data interface{} `json:"data"`
}
type ClientMessage struct {
	MessageId uint   `json:"message_id,omitempty"`
	Name      string `json:"name"`
	Avator    string `json:"avator"`
	Id        string `json:"id"`
	VisitorId string `json:"visitor_id"`
	Group     string `json:"group"`
	Time      string `json:"time"`
	ToId      string `json:"to_id"`
	Content   string `json:"content"`
	City      string `json:"city"`
	ClientIp  string `json:"client_ip"`
	Refer     string `json:"refer"`
	IsKefu    string `json:"is_kefu"`
}

var ClientList = registry.New[*User]()
var KefuList = registry.New[*User]()
var message = make(chan *Message, 256)

const (
	wsReadLimit    = 16 * 1024
	wsReadTimeout  = 90 * time.Second
	wsWriteTimeout = 10 * time.Second
)

// WriteTo 串行化单条连接的写入，并统一设置写超时。
// 所有对 User 连接的写入都必须走这里，否则半死连接会在持锁状态下阻塞到 TCP 自身超时。
func WriteTo(u *User, data []byte) error {
	if u == nil || u.Conn == nil {
		return errors.New("nil connection")
	}
	if u.AppSessionHash != "" && !models.AppSessionLeaseValid(u.AppSessionHash) {
		_ = u.Conn.Close()
		return errors.New("visitor session expired")
	}
	u.Mux.Lock()
	defer u.Mux.Unlock()
	_ = u.Conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return u.Conn.WriteMessage(websocket.TextMessage, data)
}

// WriteJSON 是 WriteTo 的便捷封装，直接写入序列化后的消息。
func WriteJSON(u *User, msg TypeMessage) error {
	str, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return WriteTo(u, str)
}

// IsKefuOnline reports whether the客服 currently has a live WebSocket connection.
func IsKefuOnline(name string) bool {
	u, ok := KefuList.Get(name)
	return ok && u != nil && u.Conn != nil
}

var upgrader = websocket.Upgrader{}

func init() {
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return common.IsAllowedOrigin(r.Header.Get("Origin"))
		},
	}
	go UpdateVisitorStatusCron()
}
func SendServerJiang(title string, content string, domain string) string {
	noticeServerJiang, err := strconv.ParseBool(models.FindConfig("NoticeServerJiang"))
	serverJiangAPI := models.FindConfig("ServerJiangAPI")
	if err != nil || !noticeServerJiang || serverJiangAPI == "" {
		return ""
	}
	sendStr := fmt.Sprintf("%s%s", title, content)
	desp := title + ":" + content + "[登录](http://" + domain + "/main)"
	url := serverJiangAPI + "?text=" + sendStr + "&desp=" + desp
	return tools.Get(url)
}

// 定时给更新数据库状态
func UpdateVisitorStatusCron() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		offline := make([]string, 0)
		for _, visitorId := range models.FindOnlineVisitorIds() {
			if visitorId == "" {
				continue
			}
			if _, ok := ClientList.Get(visitorId); !ok {
				offline = append(offline, visitorId)
			}
		}
		models.UpdateVisitorsOffline(offline)
		SendPingToKefuClient()
	}
}

// 后端广播发送消息
func WsServerBackend() {
	for msg := range message {
		var typeMsg struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(msg.content, &typeMsg); err != nil || typeMsg.Type == "" {
			continue
		}

		if msg.user != nil && msg.user.AppSessionHash != "" {
			if !models.AppSessionLeaseValid(msg.user.AppSessionHash) {
				_ = msg.user.Conn.Close()
				continue
			}
			switch typeMsg.Type {
			case "ping":
				typeMsg.Data, _ = json.Marshal("visitor:" + msg.user.Id)
			case "inputing":
				typeMsg.Data, _ = json.Marshal(map[string]string{"from": msg.user.Id, "to": msg.user.RouteTo()})
				msg.content, _ = json.Marshal(TypeMessage{Type: "inputing", Data: map[string]string{"from": msg.user.Id, "to": msg.user.RouteTo()}})
			default:
				continue
			}
		}
		switch typeMsg.Type {
		case "ping":
			var data string
			_ = json.Unmarshal(typeMsg.Data, &data)
			if len(data) > len("visitor:") && data[:len("visitor:")] == "visitor:" {
				if visitor, ok := ClientList.Get(data[len("visitor:"):]); ok && visitor != nil {
					visitor.Touch()
				}
			}
			_ = WriteJSON(msg.user, TypeMessage{Type: "pong", Data: map[string]bool{"online": IsKefuOnline(msg.user.RouteTo())}})
		case "inputing":
			var data struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(typeMsg.Data, &data); err != nil || data.From == "" || data.To == "" {
				continue
			}
			if tools.LimitFreqSingle("inputing:"+data.From, 1, 2) {
				OneKefuMessage(data.To, msg.content)
			}
		case "kfOnline", "kfConnect":
			// 客服上线 / 接手会话：把「客服已就位」推给对应访客。
			// kfOnline 的 to_id 为空时表示仅上线，没有指定访客，不需要推送。
			var data struct {
				Id     string `json:"id"`
				Name   string `json:"name"`
				Avator string `json:"avator"`
				ToId   string `json:"to_id"`
			}
			if err := json.Unmarshal(typeMsg.Data, &data); err != nil || data.ToId == "" {
				continue
			}
			visitor, ok := ClientList.Get(data.ToId)
			if !ok || visitor == nil {
				continue
			}
			// 只允许推给确实分配给该客服的访客，避免任意客服向任意访客推送。
			if msg.user == nil || visitor.RouteTo() != msg.user.Id {
				continue
			}
			_ = WriteJSON(visitor, TypeMessage{
				Type: "kfOnline",
				Data: map[string]string{
					"id":     data.Id,
					"name":   data.Name,
					"avator": data.Avator,
				},
			})
		}
	}
}

func enqueueMessage(msg *Message) bool {
	select {
	case message <- msg:
		return true
	default:
		return false
	}
}
func UpdateVisitorUser(visitorId string, toId string) {
	guest, ok := ClientList.Get(visitorId)
	if ok && guest != nil {
		guest.SetRouteTo(toId)
		_ = WriteJSON(guest, TypeMessage{Type: "transfer", Data: toId})
	}
}
