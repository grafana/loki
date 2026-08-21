package report

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFilename(t *testing.T) {
	got := Filename(time.Date(2026, 8, 20, 14, 5, 0, 0, time.UTC))
	if want := "2026-08-20-at-14-05.json"; got != want {
		t.Fatalf("Filename = %q, want %q", got, want)
	}
}

func TestCreate_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 8, 20, 14, 5, 0, 0, time.UTC)

	path, err := Create(dir, at)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if want := filepath.Join(dir, "2026-08-20-at-14-05.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	// Once the file exists, a second run at the same minute must not clobber it.
	if err := Write(path, &Report{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := Create(dir, at); err == nil {
		t.Fatalf("Create over an existing report: got nil error, want failure")
	}
}

func TestWriteLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")

	finished := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	reqs := uint64(1234)
	want := &Report{
		Description:      "dataobj run",
		LokiURL:          "http://localhost:3199",
		Tenant:           "156331",
		BackendNamespace: "loki-dev-002",
		RequestedStart:   time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
		RequestedEnd:     time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		StartedAt:        time.Date(2026, 8, 20, 14, 5, 0, 0, time.UTC),
		FinishedAt:       &finished,
		Queries: []Query{{
			Name:                "range/count_6h_5m",
			Type:                "range",
			Expr:                `sum(count_over_time({service_name=~".+"}[5m]))`,
			Start:               time.Date(2026, 8, 19, 18, 0, 0, 0, time.UTC),
			End:                 time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
			StepSeconds:         300,
			Runs:                10,
			ExecutionStartedAt:  time.Date(2026, 8, 20, 14, 5, 0, 0, time.UTC),
			ExecutionFinishedAt: time.Date(2026, 8, 20, 14, 15, 0, 0, time.UTC),
			LatenciesSeconds:    []float64{1.1, 2.2, 3.3},
			FailedRuns:          1,
			QueryStats:          QueryStats{ProcessedBytes: 987654321},
			SystemMetrics:       SystemStats{ObjstoreRequests: &reqs},
		}},
	}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestWrite_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := Write(path, &Report{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "r.json" {
		t.Fatalf("expected only r.json, got %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

func TestLoad_ConvertsTimestampsToUTC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	// A started_at with a +02:00 offset must convert (instant preserved) to the
	// equivalent UTC time, not truncate to the same wall clock in UTC.
	body := `{"started_at":"2026-08-20T14:05:00+02:00","queries":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.StartedAt.Location() != time.UTC {
		t.Errorf("StartedAt location = %v, want UTC", got.StartedAt.Location())
	}
	// 14:05 +02:00 is 12:05 UTC. A truncation bug would leave 14:05.
	if want := time.Date(2026, 8, 20, 12, 5, 0, 0, time.UTC); !got.StartedAt.Equal(want) {
		t.Errorf("StartedAt = %s, want %s (converted, not truncated)", got.StartedAt, want)
	}
}
