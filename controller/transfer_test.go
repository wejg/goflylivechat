package controller_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/router"
	"goflylivechat/tools"
	"goflylivechat/ws"
)

type transferResponse struct {
	Code   int `json:"code"`
	Result struct {
		models.Visitor
		MessageID uint                 `json:"message_id"`
		Page      int                  `json:"page"`
		Count     uint                 `json:"count"`
		List      []models.MessageKefu `json:"list"`
	} `json:"result"`
}

type transferFixture struct {
	engine   *gin.Engine
	visitor  models.Visitor
	from, to models.User
}

func newTransferFixture(t *testing.T) transferFixture {
	t.Helper()
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL test database")
	}
	if models.DB == nil {
		t.Fatal("test database is not connected")
	}
	suffix := tools.Uuid()
	f := transferFixture{
		from: models.User{Name: "transfer-a-" + suffix, Nickname: "A"},
		to:   models.User{Name: "transfer-b-" + suffix, Nickname: "B"},
	}
	f.visitor = models.Visitor{VisitorId: "transfer-" + suffix, Name: "Guest", ToId: f.from.Name, Status: 1, Avator: "/static/images/2.png", Refer: "before"}
	for _, row := range []interface{}{&f.from, &f.to, &f.visitor} {
		if err := models.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ws.ClientList.Delete(f.visitor.VisitorId)
		ws.KefuList.Delete(f.from.Name)
		ws.KefuList.Delete(f.to.Name)
		models.DB.Unscoped().Where("visitor_id=?", f.visitor.VisitorId).Delete(&models.Message{})
		models.DB.Unscoped().Where("visitor_id=?", f.visitor.VisitorId).Delete(&models.Visitor{})
		models.DB.Unscoped().Where("name IN (?)", []string{f.from.Name, f.to.Name}).Delete(&models.User{})
		models.DB.Where("user_id=?", f.from.Name).Delete(&models.ReplyItem{})
		models.DB.Where("user_id=?", f.from.Name).Delete(&models.Config{})
	})
	gin.SetMode(gin.TestMode)
	f.engine = gin.New()
	router.InitApiRouter(f.engine)
	return f
}

func (f transferFixture) request(t *testing.T, method, endpoint, actor string, values url.Values) transferResponse {
	t.Helper()
	req := httptest.NewRequest(method, endpoint, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.1:1234"
	if actor != "" {
		token, err := tools.MakeToken(map[string]interface{}{"kefu_name": actor, "kefu_id": f.from.ID, "create_time": time.Now().Unix()})
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("token", token)
		req.RemoteAddr = "192.0.2.2:1234"
	}
	tools.LimitQueue.Lock()
	delete(tools.LimitQueue.LimitQueue, "sendmessage:"+strings.Split(req.RemoteAddr, ":")[0])
	tools.LimitQueue.Unlock()
	w := httptest.NewRecorder()
	f.engine.ServeHTTP(w, req)
	var response transferResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("HTTP %d %s: %v", w.Code, w.Body.String(), err)
	}
	return response
}

func (f transferFixture) transfer(t *testing.T, actor string) transferResponse {
	t.Helper()
	return f.request(t, "GET", "/trans_kefu?"+url.Values{"kefu_id": {f.to.Name}, "visitor_id": {f.visitor.VisitorId}}.Encode(), actor, nil)
}

func transferSocket(t *testing.T, id, route string, visitor bool) *websocket.Conn {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	conn := <-accepted
	user := &ws.User{Id: id, To_id: route, Conn: conn, UpdateTime: time.Now()}
	if visitor {
		ws.ClientList.Set(id, user)
	} else {
		ws.KefuList.Set(id, user)
	}
	t.Cleanup(func() {
		if visitor {
			ws.ClientList.Delete(id)
		} else {
			ws.KefuList.Delete(id)
		}
		client.Close()
		conn.Close()
		server.Close()
	})
	return client
}

func transferEvent(t *testing.T, conn *websocket.Conn, kind string) json.RawMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		if event.Type == kind {
			return event.Data
		}
	}
}

func TestTransferNotifiesVisitorAndKeepsOnlineStatus(t *testing.T) {
	f := newTransferFixture(t)
	oldAgent := transferSocket(t, f.from.Name, "", false)
	newAgent := transferSocket(t, f.to.Name, "", false)
	guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
	if response := f.transfer(t, f.from.Name); response.Code != 200 {
		t.Fatalf("transfer: %+v", response)
	}
	transferEvent(t, oldAgent, "userOffline")
	transferEvent(t, newAgent, "userOnline")
	visitor := models.FindVisitorByVistorId(f.visitor.VisitorId)
	if visitor.ToId != f.to.Name || visitor.Status != 1 {
		t.Errorf("transferred online visitor: %+v", visitor)
	}
	var target string
	if err := json.Unmarshal(transferEvent(t, guest, "transfer"), &target); err != nil {
		t.Fatal(err)
	}
	if target != f.to.Name {
		t.Fatalf("visitor routed to %q", target)
	}
	response := f.request(t, "POST", "/2/message", "", url.Values{"from_id": {f.visitor.VisitorId}, "to_id": {target}, "content": {"after transfer"}, "type": {"visitor"}})
	if response.Code != 200 {
		t.Fatalf("message after transfer: %+v", response)
	}
	transferEvent(t, newAgent, "message")
}

func TestTransferRejectsAnotherAgentsVisitor(t *testing.T) {
	f := newTransferFixture(t)
	response := f.transfer(t, f.to.Name)
	if response.Code != 403 {
		t.Fatalf("another agent transferred visitor: %+v", response)
	}
	if visitor := models.FindVisitorByVistorId(f.visitor.VisitorId); visitor.ToId != f.from.Name {
		t.Fatalf("ownership changed: %+v", visitor)
	}
}

// A real read-only transaction lets reads succeed while writes fail at MySQL.
func transferReadOnlyDatabase(t *testing.T) {
	t.Helper()
	config := common.GetMysqlConf()
	db, err := gorm.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", config.Username, config.Password, config.Server, config.Port, config.Database))
	if err != nil {
		t.Fatal(err)
	}
	db.SingularTable(true)
	db.DB().SetMaxOpenConns(1)
	if err := db.Exec("SET SESSION TRANSACTION READ ONLY").Error; err != nil {
		db.Close()
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		db.Close()
		t.Fatal(tx.Error)
	}
	original := models.DB
	models.DB = tx
	t.Cleanup(func() { tx.Rollback(); models.DB = original; db.Close() })
}

func TestTransferFailsWhenDatabaseCannotSaveRoute(t *testing.T) {
	f := newTransferFixture(t)
	transferReadOnlyDatabase(t)
	if response := f.transfer(t, f.from.Name); response.Code != 500 {
		t.Fatalf("unsaved transfer reported success: %+v", response)
	}
}

func TestVisitorLoginPreservesTransferredRouteAndReturnsSavedRecord(t *testing.T) {
	f := newTransferFixture(t)
	if err := models.DB.Model(&models.Visitor{}).Where("visitor_id=?", f.visitor.VisitorId).Update("to_id", f.to.Name).Error; err != nil {
		t.Fatal(err)
	}
	newAgent := transferSocket(t, f.to.Name, "", false)
	response := f.request(t, "POST", "/visitor_login", "", url.Values{"visitor_id": {f.visitor.VisitorId}, "to_id": {f.from.Name}, "refer": {"after"}})
	if response.Code != 200 || response.Result.ToId != f.to.Name || response.Result.Refer != "after" || response.Result.ID != f.visitor.ID {
		t.Fatalf("stale entry overrode saved visitor: %+v", response)
	}
	transferEvent(t, newAgent, "userOnline")
}

func TestVisitorLoginReturnsNewVisitorDatabaseFields(t *testing.T) {
	f := newTransferFixture(t)
	if err := models.DB.Unscoped().Delete(&f.visitor).Error; err != nil {
		t.Fatal(err)
	}
	response := f.request(t, "POST", "/visitor_login", "", url.Values{"visitor_id": {f.visitor.VisitorId}, "to_id": {f.from.Name}, "refer": {"created"}})
	if response.Code != 200 || response.Result.ID == 0 || response.Result.CreatedAt.IsZero() || response.Result.Refer != "created" {
		t.Fatalf("new visitor not returned from database: %+v", response)
	}
}

func TestMessageResponsesAndPushesUseSavedMessageIDs(t *testing.T) {
	f := newTransferFixture(t)
	agent := transferSocket(t, f.from.Name, "", false)
	guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
	for _, item := range []struct {
		endpoint, actor, from, to, kind string
		conn                            *websocket.Conn
	}{
		{"/2/message", "", f.visitor.VisitorId, f.from.Name, "visitor", agent},
		{"/kefu/message", f.from.Name, f.from.Name, f.visitor.VisitorId, "kefu", guest},
	} {
		response := f.request(t, "POST", item.endpoint, item.actor, url.Values{"from_id": {item.from}, "to_id": {item.to}, "type": {item.kind}, "content": {"saved " + item.kind}})
		var pushed ws.ClientMessage
		if err := json.Unmarshal(transferEvent(t, item.conn, "message"), &pushed); err != nil {
			t.Fatal(err)
		}
		var saved models.Message
		models.DB.Where("visitor_id=? AND mes_type=?", f.visitor.VisitorId, item.kind).Last(&saved)
		if response.Code != 200 || saved.ID == 0 || response.Result.MessageID != saved.ID || pushed.MessageId != saved.ID {
			t.Errorf("message IDs: response=%+v pushed=%+v saved=%+v", response, pushed, saved)
		}
		if item.kind == "kefu" {
			var echo ws.ClientMessage
			if err := json.Unmarshal(transferEvent(t, agent, "message"), &echo); err != nil {
				t.Fatal(err)
			}
			if echo.MessageId != saved.ID {
				t.Errorf("agent echo lost message ID: %+v", echo)
			}
		}
	}
}

func TestAutoReplyPushesOnlySavedMessageWithItsID(t *testing.T) {
	f := newTransferFixture(t)
	agent := transferSocket(t, f.from.Name, "", false)
	guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
	reply := models.ReplyItem{UserId: f.from.Name, GroupId: "1", ItemName: "hello", Content: "automatic answer"}
	if err := models.DB.Create(&reply).Error; err != nil {
		t.Fatal(err)
	}
	ws.VisitorAutoReply(f.visitor, f.from, "hello")
	var saved models.Message
	models.DB.Where("visitor_id=? AND mes_type='kefu'", f.visitor.VisitorId).Last(&saved)
	for _, conn := range []*websocket.Conn{agent, guest} {
		var pushed ws.ClientMessage
		if err := json.Unmarshal(transferEvent(t, conn, "message"), &pushed); err != nil {
			t.Fatal(err)
		}
		if saved.ID == 0 || pushed.MessageId != saved.ID {
			t.Errorf("auto reply ID: pushed=%+v saved=%+v", pushed, saved)
		}
	}
}

func TestVisitorLoginFailsWhenDatabaseCannotSave(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			f := newTransferFixture(t)
			if !existing {
				if err := models.DB.Unscoped().Delete(&f.visitor).Error; err != nil {
					t.Fatal(err)
				}
			}
			transferReadOnlyDatabase(t)
			response := f.request(t, "POST", "/visitor_login", "", url.Values{"visitor_id": {f.visitor.VisitorId}, "to_id": {f.from.Name}, "refer": {"unsaved"}})
			if response.Code != 500 {
				t.Fatalf("unsaved login reported success: %+v", response)
			}
		})
	}
}

func TestAutoReplyDoesNotPushWhenSaveFails(t *testing.T) {
	f := newTransferFixture(t)
	agent := transferSocket(t, f.from.Name, "", false)
	guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
	reply := models.ReplyItem{UserId: f.from.Name, GroupId: "1", ItemName: "hello", Content: "unsaved answer"}
	if err := models.DB.Create(&reply).Error; err != nil {
		t.Fatal(err)
	}
	transferReadOnlyDatabase(t)
	ws.VisitorAutoReply(f.visitor, f.from, "hello")
	for _, conn := range []*websocket.Conn{agent, guest} {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Error("unsaved auto reply was pushed")
		}
	}
}

func TestOfflineAutoReplyUsesSavedIDAndDoesNotPushOnSaveFailure(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(readOnly), func(t *testing.T) {
			f := newTransferFixture(t)
			guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
			config := models.Config{UserId: f.from.Name, ConfKey: "OfflineMessage", ConfValue: "offline answer"}
			if err := models.DB.Create(&config).Error; err != nil {
				t.Fatal(err)
			}
			if readOnly {
				transferReadOnlyDatabase(t)
			}
			ws.VisitorAutoReply(f.visitor, f.from, "no matching reply")
			if readOnly {
				guest.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				if _, _, err := guest.ReadMessage(); err == nil {
					t.Error("unsaved offline answer was pushed")
				}
				return
			}
			var pushed ws.ClientMessage
			if err := json.Unmarshal(transferEvent(t, guest, "message"), &pushed); err != nil {
				t.Fatal(err)
			}
			var saved models.Message
			models.DB.Where("visitor_id=? AND mes_type='kefu'", f.visitor.VisitorId).Last(&saved)
			if saved.ID == 0 || pushed.MessageId != saved.ID {
				t.Errorf("offline reply ID: pushed=%+v saved=%+v", pushed, saved)
			}
		})
	}
}

func TestConcurrentTransfersKeepDatabaseLiveRouteAndEventsInOrder(t *testing.T) {
	f := newTransferFixture(t)
	third := models.User{Name: "transfer-c-" + tools.Uuid(), Nickname: "C"}
	if err := models.DB.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { models.DB.Unscoped().Delete(&third) })
	transferSocket(t, f.from.Name, "", false)
	middleAgent := transferSocket(t, f.to.Name, "", false)
	transferSocket(t, third.Name, "", false)
	guest := transferSocket(t, f.visitor.VisitorId, f.from.Name, true)
	committed, release := make(chan struct{}), make(chan struct{})
	releaseFirst := sync.OnceFunc(func() { close(release) })
	models.DB.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:pause_transfer_commit", func(scope *gorm.Scope) {
		attrs, _ := scope.InstanceGet("gorm:update_attrs")
		updates, _ := attrs.(map[string]interface{})
		if scope.TableName() == "visitor" && updates["to_id"] == f.to.Name {
			close(committed)
			<-release
		}
	})
	t.Cleanup(func() { releaseFirst(); models.DB.Callback().Update().Remove("test:pause_transfer_commit") })
	first := make(chan transferResponse, 1)
	go func() { first <- f.transfer(t, f.from.Name) }()
	select {
	case <-committed:
	case <-time.After(2 * time.Second):
		t.Fatal("first transfer did not reach committed database update")
	}
	second := make(chan transferResponse, 1)
	go func() {
		endpoint := "/trans_kefu?" + url.Values{"visitor_id": {f.visitor.VisitorId}, "kefu_id": {third.Name}}.Encode()
		second <- f.request(t, "GET", endpoint, f.to.Name, nil)
	}()
	var secondResponse transferResponse
	select {
	case secondResponse = <-second:
		t.Error("later transfer finished before earlier route and notifications were applied")
	case <-time.After(200 * time.Millisecond):
	}
	releaseFirst()
	if response := <-first; response.Code != 200 {
		t.Fatalf("first transfer: %+v", response)
	}
	if secondResponse.Code == 0 {
		select {
		case secondResponse = <-second:
		case <-time.After(2 * time.Second):
			t.Fatal("later transfer remained blocked")
		}
	}
	if secondResponse.Code != 200 {
		t.Fatalf("second transfer: %+v", secondResponse)
	}
	visitor := models.FindVisitorByVistorId(f.visitor.VisitorId)
	live, _ := ws.ClientList.Get(f.visitor.VisitorId)
	if visitor.ToId != third.Name || live.RouteTo() != third.Name || visitor.Status != 1 {
		t.Errorf("database=%s live=%s status=%d", visitor.ToId, live.RouteTo(), visitor.Status)
	}
	for _, want := range []string{f.to.Name, third.Name} {
		var target string
		if err := json.Unmarshal(transferEvent(t, guest, "transfer"), &target); err != nil {
			t.Fatal(err)
		}
		if target != want {
			t.Errorf("transfer event target=%s want=%s", target, want)
		}
	}
	middleAgent.SetReadDeadline(time.Now().Add(2 * time.Second))
	for _, want := range []string{"userOnline", "userOffline"} {
		var event struct {
			Type string `json:"type"`
		}
		if err := middleAgent.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type != want {
			t.Errorf("middle agent event=%s want=%s", event.Type, want)
		}
	}
}

func TestMessageClientIDIsIdempotent(t *testing.T) {
	f := newTransferFixture(t)
	values := url.Values{
		"from_id":           {f.visitor.VisitorId},
		"to_id":             {f.from.Name},
		"content":           {"retry-safe"},
		"type":              {"visitor"},
		"client_message_id": {"retry-key-" + tools.Uuid()},
	}
	first := f.request(t, "POST", "/2/message", "", values)
	second := f.request(t, "POST", "/2/message", "", values)
	if first.Code != 200 || second.Code != 200 || first.Result.MessageID == 0 || first.Result.MessageID != second.Result.MessageID {
		t.Fatalf("idempotent send: first=%+v second=%+v", first, second)
	}
	if got := models.CountMessage("visitor_id=? AND client_message_id=?", f.visitor.VisitorId, values.Get("client_message_id")); got != 1 {
		t.Fatalf("saved message count=%d, want 1", got)
	}
}
