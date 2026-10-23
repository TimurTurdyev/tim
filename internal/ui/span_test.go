package ui

import (
	"testing"
	"time"
)

func TestParseSpanAndSince(t *testing.T) {
	for in, want := range map[string]time.Duration{"30s": 30 * time.Second, "15m": 15 * time.Minute, "24h": 24 * time.Hour, "3d": 72 * time.Hour, "1w": 7 * 24 * time.Hour} {
		if got, err := ParseSpan(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "0d", "-3d", "вчера", "3 d"} {
		if _, err := ParseSpan(bad); err == nil {
			t.Errorf("%q разобрался, а не должен", bad)
		}
	}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local)
	if got, err := ParseSince("2026-10-01", now); err != nil || got.Day() != 1 || got.Month() != 10 {
		t.Errorf("дата: %v %v", got, err)
	}
	if got, err := ParseSince("1w", now); err != nil || !got.Equal(now.AddDate(0, 0, -7)) {
		t.Errorf("срок: %v %v", got, err)
	}
}
