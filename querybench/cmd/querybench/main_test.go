package main

import (
	"testing"
	"time"
)

func TestParseTime_ReturnsUTC(t *testing.T) {
	// 02:00 +02:00 is the same instant as 00:00Z.
	want := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for _, in := range []string{"1787184000", "2026-08-20T00:00:00Z", "2026-08-20T02:00:00+02:00"} {
		got, err := parseTime(in)
		if err != nil {
			t.Errorf("parseTime(%q): %v", in, err)
			continue
		}
		if got.Location() != time.UTC {
			t.Errorf("parseTime(%q) location = %v, want UTC", in, got.Location())
		}
		if in != "1787184000" && !got.Equal(want) {
			t.Errorf("parseTime(%q) = %s, want %s", in, got, want)
		}
	}
	if _, err := parseTime("not-a-time"); err == nil {
		t.Error(`parseTime("not-a-time") should error`)
	}
}
