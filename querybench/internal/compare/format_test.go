package compare

import (
	"math"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	cases := map[float64]string{
		0:          "0 B",
		999:        "999 B",
		1000:       "1 KB",
		1536000:    "1.54 MB",
		1200000000: "1.2 GB",
		-2000:      "-2 KB",
	}
	for v, want := range cases {
		if got := formatBytes(v); got != want {
			t.Errorf("formatBytes(%g) = %q, want %q", v, got, want)
		}
	}
}

func TestFormatCount(t *testing.T) {
	cases := map[float64]string{
		500:     "500",
		1500:    "1.5 K",
		2000000: "2 M",
	}
	for v, want := range cases {
		if got := formatCount(v); got != want {
			t.Errorf("formatCount(%g) = %q, want %q", v, got, want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[float64]string{
		0.045: "45 ms",
		1.5:   "1.50 s",
	}
	for v, want := range cases {
		if got := formatDuration(v); got != want {
			t.Errorf("formatDuration(%g) = %q, want %q", v, got, want)
		}
	}
	if got := formatDuration(math.NaN()); got != "–" {
		t.Errorf("formatDuration(NaN) = %q, want –", got)
	}
}

func TestFormatCoresAndRate(t *testing.T) {
	if got := formatCores(2.5); got != "2.50" {
		t.Errorf("formatCores(2.5) = %q", got)
	}
	if got := formatBytesPerSecond(1000); got != "1 KB/s" {
		t.Errorf("formatBytesPerSecond(1000) = %q", got)
	}
}

func TestShortDuration(t *testing.T) {
	cases := map[time.Duration]string{
		6 * time.Hour:      "6h",
		5 * time.Minute:    "5m",
		90 * time.Second:   "1m30s",
		time.Hour:          "1h",
		1455 * time.Minute: "24h15m",
	}
	for d, want := range cases {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
