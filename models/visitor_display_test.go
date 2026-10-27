package models

import (
	"encoding/base64"
	"testing"
)

func TestVisitorDisplayName(t *testing.T) {
	for _, item := range []struct {
		name, extra, want string
	}{
		{"App visitor", `{"account_id":"17425","is_guest":"true"}`, "visitor_17425"},
		{"游客昵称", `{"account_id":"42","is_guest":"true"}`, "visitor_42"},
		{"user_2088", `{"account_id":"2088","is_guest":"false"}`, "user_2088"},
		{"手机用户", `{"account_id":"42","is_guest":"false"}`, "手机用户"},
		{"中文昵称", `{"account_id":"42"}`, "中文昵称"},
		{"App visitor", `{"account_id":"42","is_guest":"false"}`, "App visitor"},
		{"App visitor", `{"account_id":"42","is_guest":"invalid"}`, "App visitor"},
		{"App visitor", `{"account_id":"42","is_guest":true}`, "App visitor"},
		{"访客", `{}`, "访客"},
		{"访客", `{"account_id":"","is_guest":"true"}`, "访客"},
		{"访客", `invalid`, "访客"},
	} {
		t.Run(item.name+item.extra, func(t *testing.T) {
			for _, encoding := range []*base64.Encoding{base64.RawStdEncoding, base64.StdEncoding} {
				visitor := Visitor{Model: Model{ID: 1012}, Name: item.name, Extra: encoding.EncodeToString([]byte(item.extra))}
				if got := visitor.DisplayName(); got != item.want {
					t.Errorf("display name = %q, want %q", got, item.want)
				}
				if visitor.Name != item.name {
					t.Fatal("display formatting changed the stored name")
				}
			}
		})
	}
}

func TestVisitorDisplayNameAfterGuestBindsAccount(t *testing.T) {
	visitor := Visitor{Model: Model{ID: 1012}, Name: "App visitor", Extra: base64.RawStdEncoding.EncodeToString([]byte(`{"account_id":"17425","is_guest":"true"}`))}
	if got := visitor.DisplayName(); got != "visitor_17425" {
		t.Fatalf("guest name: %q", got)
	}
	visitor.Name = "user_17425"
	visitor.Extra = base64.RawStdEncoding.EncodeToString([]byte(`{"account_id":"17425","is_guest":"false"}`))
	if got := visitor.DisplayName(); got != "user_17425" {
		t.Fatalf("registered account name: %q", got)
	}
}
