package controller_test

import (
	"fmt"
	"net/url"
	"testing"

	"goflylivechat/models"
)

func TestHistoryCursorDoesNotRepeatOrSkipOlderMessagesAfterNewMessagesArrive(t *testing.T) {
	f := newTransferFixture(t)
	messages := make([]models.Message, 20)
	for i := range messages[:15] {
		messages[i] = models.Message{VisitorId: f.visitor.VisitorId, KefuId: f.from.Name, MesType: "visitor", Content: fmt.Sprint(i), Status: "read"}
		if err := models.DB.Create(&messages[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	first := f.request(t, "GET", "/2/messagesPages?"+url.Values{"visitor_id": {f.visitor.VisitorId}, "pagesize": {"5"}}.Encode(), f.from.Name, nil)
	if first.Code != 200 || first.Result.Count != 15 || len(first.Result.List) != 5 || first.Result.List[0].ID != messages[14].ID || first.Result.List[4].ID != messages[10].ID {
		t.Fatalf("initial history: %+v", first)
	}
	for i := 15; i < len(messages); i++ {
		messages[i] = models.Message{VisitorId: f.visitor.VisitorId, KefuId: f.from.Name, MesType: "kefu", Content: fmt.Sprint(i), Status: "read"}
		if err := models.DB.Create(&messages[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		page, before string
		newest       int
	}{
		{"2", fmt.Sprint(first.Result.List[4].ID), 9},
		{"77", fmt.Sprint(messages[5].ID), 4},
	} {
		response := f.request(t, "GET", "/2/messagesPages?"+url.Values{"visitor_id": {f.visitor.VisitorId}, "pagesize": {"5"}, "page": {item.page}, "before_id": {item.before}}.Encode(), f.from.Name, nil)
		if response.Code != 200 || response.Result.Count != 20 || fmt.Sprint(response.Result.Page) != item.page || len(response.Result.List) != 5 {
			t.Fatalf("cursor history: %+v", response)
		}
		for i, message := range response.Result.List {
			if message.ID != messages[item.newest-i].ID {
				t.Errorf("cursor=%s row=%d got=%d want=%d", item.before, i, message.ID, messages[item.newest-i].ID)
			}
		}
	}
	legacy := f.request(t, "GET", "/2/messagesPages?"+url.Values{"visitor_id": {f.visitor.VisitorId}, "pagesize": {"5"}, "page": {"2"}}.Encode(), f.from.Name, nil)
	if legacy.Code != 200 || len(legacy.Result.List) != 5 || legacy.Result.List[0].ID != messages[14].ID || legacy.Result.List[4].ID != messages[10].ID {
		t.Fatalf("legacy offset page changed: %+v", legacy)
	}
}

func TestHistoryCursorRejectsInvalidIDs(t *testing.T) {
	f := newTransferFixture(t)
	for _, cursor := range []string{"", "0", "-1", "oops", "1.5", "4294967296"} {
		response := f.request(t, "GET", "/2/messagesPages?"+url.Values{"visitor_id": {f.visitor.VisitorId}, "before_id": {cursor}}.Encode(), f.from.Name, nil)
		if response.Code != 400 {
			t.Errorf("cursor %q accepted: %+v", cursor, response)
		}
	}
}
