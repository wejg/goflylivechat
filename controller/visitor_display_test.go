package controller_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"goflylivechat/models"
	"goflylivechat/tools"
	"goflylivechat/ws"
)

func TestVisitorShortNameAcrossDetailsListsAndOnlineEvents(t *testing.T) {
	for _, account := range []struct {
		kind, name, extra, want string
	}{
		{"guest", "App visitor", `{"account_id":"17425","is_guest":"true"}`, "visitor_17425"},
		{"email", "user_2088", `{"account_id":"2088","is_guest":"false"}`, "user_2088"},
		{"phone", "手机用户", `{"account_id":"42","is_guest":"false"}`, "手机用户"},
	} {
		t.Run(account.kind, func(t *testing.T) {
			f := newTransferFixture(t)
			f.visitor.Name = account.name
			f.visitor.Extra = base64.RawStdEncoding.EncodeToString([]byte(account.extra))
			if err := models.DB.Model(&f.visitor).Updates(map[string]interface{}{"name": f.visitor.Name, "extra": f.visitor.Extra}).Error; err != nil {
				t.Fatal(err)
			}
			want := account.want
			details := f.request(t, "GET", "/visitor?"+url.Values{"visitorId": {f.visitor.VisitorId}}.Encode(), f.from.Name, nil)
			if details.Code != 200 || details.Result.Name != want {
				t.Fatalf("visitor details: %+v", details)
			}
			agent := transferSocket(t, f.from.Name, "", false)
			server := httptest.NewServer(f.engine)
			defer server.Close()
			guest, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws_visitor?"+url.Values{"visitor_id": {f.visitor.VisitorId}}.Encode(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer guest.Close()
			for _, notify := range []func(){func() {}, func() { ws.VisitorOnline(f.from.Name, f.visitor) }} {
				notify()
				var online map[string]string
				if err := json.Unmarshal(transferEvent(t, agent, "userOnline"), &online); err != nil || online["username"] != want {
					t.Fatalf("online notification: %v, %v", online, err)
				}
			}
			token, err := tools.MakeToken(map[string]interface{}{"kefu_name": f.from.Name, "kefu_id": f.from.ID, "create_time": time.Now().Unix()})
			if err != nil {
				t.Fatal(err)
			}
			for _, endpoint := range []string{"/visitors", "/visitors_kefu_online"} {
				req := httptest.NewRequest("GET", endpoint, nil)
				req.Header.Set("token", token)
				recorder := httptest.NewRecorder()
				f.engine.ServeHTTP(recorder, req)
				if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"`+want+`"`) {
					t.Fatalf("%s lacks short name: %s", endpoint, recorder.Body.String())
				}
			}
			stored := models.FindVisitorByVistorId(f.visitor.VisitorId)
			if stored.Name != f.visitor.Name {
				t.Fatalf("stored nickname changed from %q to %q", f.visitor.Name, stored.Name)
			}
		})
	}
}
