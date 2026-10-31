package common

import (
	"testing"
	"time"
)

func TestServiceTime(t *testing.T) {
	for _, tc := range []struct {
		at   string
		open bool
	}{
		{"2026-11-01T09:59:59+08:00", false},
		{"2026-11-01T10:00:00+08:00", true},
		{"2026-11-01T17:59:59+08:00", true},
		{"2026-11-01T18:00:00+08:00", false},
		{"2026-11-01T02:00:00Z", true},
		{"2026-11-01T10:00:00Z", false},
	} {
		now, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if got := IsServiceTime(now); got != tc.open {
			t.Errorf("%s: got %v, want %v", tc.at, got, tc.open)
		}
	}
}
