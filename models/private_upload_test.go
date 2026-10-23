package models

import (
	"os"
	"testing"
)

func TestPrivateUploadFollowsConversationOwnership(t *testing.T) {
	if os.Getenv("GOFLY_TEST_DB") != "1" {
		t.Skip("requires isolated MySQL")
	}
	if err := EnsureUploadSchema(); err != nil {
		t.Fatal(err)
	}
	visitor := Visitor{VisitorId: "upload-owner-test", ToId: "agent-a", Name: "Guest"}
	if err := DB.Create(&visitor).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		DB.Unscoped().Where("visitor_id=?", visitor.VisitorId).Delete(&Visitor{})
		DB.Where("visitor_id=?", visitor.VisitorId).Delete(&PrivateUpload{})
	})
	upload, err := RecordPrivateUpload("static/upload/test.pdf", "invoice.pdf", visitor.VisitorId, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		visitor, agent string
		want           bool
	}{
		{visitor.VisitorId, "", true}, {"other", "", false}, {"", "agent-a", true}, {"", "agent-b", false}, {"", "", false},
	} {
		got, err := CanReadPrivateUpload(upload.ID, tc.visitor, tc.agent)
		if err != nil || got != tc.want {
			t.Fatalf("actor %q %q: got %v err %v", tc.visitor, tc.agent, got, err)
		}
	}
	if err := DB.Model(&visitor).Update("to_id", "agent-b").Error; err != nil {
		t.Fatal(err)
	}
	if got, _ := CanReadPrivateUpload(upload.ID, "", "agent-a"); got {
		t.Fatal("previous agent retained attachment access after transfer")
	}
	if got, _ := CanReadPrivateUpload(upload.ID, "", "agent-b"); !got {
		t.Fatal("new agent cannot read attachment")
	}
}
