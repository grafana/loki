package compactor

import (
	"testing"
	"time"
)

func TestMarkerPath_Deterministic(t *testing.T) {
	window := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	got1 := MarkerPath(DefaultInFlightPrefix, "29", window, 1)
	got2 := MarkerPath(DefaultInFlightPrefix, "29", window, 1)
	if got1 != got2 {
		t.Fatalf("MarkerPath not deterministic: %q vs %q", got1, got2)
	}
}

func TestMarkerPath_StructureAndPrefix(t *testing.T) {
	window := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	p := MarkerPath("dataobj/compaction/in-flight/", "29", window, 1)
	const wantPrefix = "dataobj/compaction/in-flight/"
	if p[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("path missing prefix: %q", p)
	}
	if got := len(p) - len(wantPrefix) - len(".json"); got != 64 {
		t.Fatalf("hex sha256 expected 64 chars, got %d in %q", got, p)
	}
	if p[len(p)-5:] != ".json" {
		t.Fatalf("path missing .json suffix: %q", p)
	}
}

func TestMarkerPath_DiffersOnInputs(t *testing.T) {
	window := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	base := MarkerPath(DefaultInFlightPrefix, "29", window, 1)
	cases := map[string]string{
		"different tenant":      MarkerPath(DefaultInFlightPrefix, "30", window, 1),
		"different window":      MarkerPath(DefaultInFlightPrefix, "29", window.Add(time.Hour), 1),
		"different planVersion": MarkerPath(DefaultInFlightPrefix, "29", window, 2),
	}
	for name, other := range cases {
		if other == base {
			t.Errorf("%s: expected different path, got identical %q", name, base)
		}
	}
}

func TestMarkerPath_TimezoneNormalized(t *testing.T) {
	utc := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	tz, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	pacific := utc.In(tz) // same instant, different zone
	if MarkerPath(DefaultInFlightPrefix, "29", utc, 1) != MarkerPath(DefaultInFlightPrefix, "29", pacific, 1) {
		t.Fatalf("MarkerPath should normalize to UTC")
	}
}
