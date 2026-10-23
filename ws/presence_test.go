package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestVisitorHeartbeatReportsAssignedAgentPresence(t *testing.T) {
	const agent = "heartbeat-presence-test-agent"
	connected := make(chan *User, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connected <- &User{Conn: conn, To_id: agent}
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	visitor := <-connected
	defer visitor.Conn.Close()
	defer KefuList.Delete(agent)
	go WsServerBackend()
	for _, online := range []bool{false, true, false} {
		if online {
			KefuList.Set(agent, &User{Conn: visitor.Conn})
		} else {
			KefuList.Delete(agent)
		}
		if !enqueueMessage(&Message{user: visitor, content: []byte(`{"type":"ping"}`)}) {
			t.Fatal("heartbeat queue full")
		}
		client.SetReadDeadline(time.Now().Add(time.Second))
		var reply struct {
			Type string `json:"type"`
			Data struct {
				Online bool `json:"online"`
			} `json:"data"`
		}
		if err := client.ReadJSON(&reply); err != nil {
			t.Fatal(err)
		}
		if reply.Type != "pong" || reply.Data.Online != online {
			t.Fatalf("online=%v: got %+v", online, reply)
		}
	}
}
