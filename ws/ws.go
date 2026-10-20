package ws

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"goflylivechat/common"
	"goflylivechat/internal/registry"
	"goflylivechat/models"
	"goflylivechat/tools"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type User struct {
	Conn       *websocket.Conn
	Name       string
	Id         string
	Avator     string
	To_id      string
	Role_id    string
	Mux        sync.Mutex
	UpdateTime time.Time
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
	conn        *websocket.Conn
	context     *gin.Context
	content     []byte
	messageType int
	Mux         sync.Mutex
}
type TypeMessage struct {
	Type interface{} `json:"type"`
	Data interface{} `json:"data"`
}
type ClientMessage struct {
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
	wsReadLimit   = 16 * 1024
	wsReadTimeout = 90 * time.Second
)

var upgrader = websocket.Upgrader{}
var Mux sync.RWMutex

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
		log.Println("do not notice serverjiang:", serverJiangAPI, noticeServerJiang)
		return ""
	}
	sendStr := fmt.Sprintf("%s%s", title, content)
	desp := title + ":" + content + "[登录](http://" + domain + "/main)"
	url := serverJiangAPI + "?text=" + sendStr + "&desp=" + desp
	//log.Println(url)
	res := tools.Get(url)
	return res
}
func SendFlyServerJiang(title string, content string, domain string) string {
	return ""
}

// 定时给更新数据库状态
func UpdateVisitorStatusCron() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		visitors := models.FindVisitorsOnline()
		for _, visitor := range visitors {
			if visitor.VisitorId == "" {
				continue
			}
			if _, ok := ClientList.Get(visitor.VisitorId); !ok {
				models.UpdateVisitorStatus(visitor.VisitorId, 0)
			}
		}
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

		switch typeMsg.Type {
		case "ping":
			var data string
			_ = json.Unmarshal(typeMsg.Data, &data)
			if len(data) > len("visitor:") && data[:len("visitor:")] == "visitor:" {
				if visitor, ok := ClientList.Get(data[len("visitor:"):]); ok && visitor != nil {
					visitor.Touch()
				}
			}
			str, _ := json.Marshal(TypeMessage{Type: "pong"})
			msg.Mux.Lock()
			_ = msg.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = msg.conn.WriteMessage(websocket.TextMessage, str)
			msg.Mux.Unlock()
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
	if ok {
		guest.SetRouteTo(toId)
	}
}
