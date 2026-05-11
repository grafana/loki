package compactor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/thanos-io/objstore"
)

func sampleMarker(t *testing.T) (Marker, []byte) {
	t.Helper()
	m := Marker{
		WorkflowID:          "wf-abc",
		Tenant:              "29",
		Window:              time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
		PlanVersion:         1,
		StartedAt:           time.Date(2026, 5, 8, 15, 23, 14, 0, time.UTC),
		ExpectedLogObjects:  2667,
		ExpectedIndexObject: "tenants/29/objects/abc.dataobj",
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return m, b
}

func TestWriteMarker_CreatesNew(t *testing.T) {
	bkt := objstore.NewInMemBucket()
	_, content := sampleMarker(t)
	path := MarkerPath(DefaultInFlightPrefix, "29", time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), 1)

	created, err := WriteMarker(context.Background(), bkt, path, content)
	if err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first write")
	}

	rc, err := bkt.Get(context.Background(), path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("stored content mismatch:\n got=%s\nwant=%s", got, content)
	}
}

func TestWriteMarker_SoftIdempotency_IdenticalContent(t *testing.T) {
	bkt := objstore.NewInMemBucket()
	_, content := sampleMarker(t)
	path := MarkerPath(DefaultInFlightPrefix, "29", time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), 1)

	if _, err := WriteMarker(context.Background(), bkt, path, content); err != nil {
		t.Fatalf("first write: %v", err)
	}
	created, err := WriteMarker(context.Background(), bkt, path, content)
	if err != nil {
		t.Fatalf("second write returned error: %v", err)
	}
	if created {
		t.Fatalf("expected created=false on identical second write")
	}
}

func TestWriteMarker_RaceLoss_DifferentContent(t *testing.T) {
	bkt := objstore.NewInMemBucket()
	_, contentA := sampleMarker(t)
	mB, _ := sampleMarker(t)
	mB.WorkflowID = "wf-different"
	contentB, err := json.Marshal(mB)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := MarkerPath(DefaultInFlightPrefix, "29", time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), 1)

	if _, err := WriteMarker(context.Background(), bkt, path, contentA); err != nil {
		t.Fatalf("first write: %v", err)
	}
	created, err := WriteMarker(context.Background(), bkt, path, contentB)
	if err != nil {
		t.Fatalf("second write returned error: %v", err)
	}
	if created {
		t.Fatalf("expected created=false when an existing marker is present")
	}

	rc, err := bkt.Get(context.Background(), path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, contentA) {
		t.Fatalf("existing marker was overwritten\n got=%s\nwant=%s", got, contentA)
	}
}

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
