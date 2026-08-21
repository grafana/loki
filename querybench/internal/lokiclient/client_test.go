package lokiclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/queries"
)

const statsBody = `{"data":{"result":[],"stats":{"summary":{"totalBytesProcessed":123456}}}}`

func TestRun_RangeRequest(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	var gotOrg, gotCache, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotOrg = r.Header.Get("X-Scope-OrgID")
		gotCache = r.Header.Get("Cache-Control")
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(statsBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "156331", Options{})
	q := queries.Query{Name: "r", Type: queries.TypeRange, Expr: "sum(x)", Step: 5 * time.Minute}
	start := time.Unix(1000, 0)
	end := time.Unix(4600, 0)

	res, err := c.Run(context.Background(), q, start, end)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPath != "/loki/api/v1/query_range" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery.Get("query") != "sum(x)" {
		t.Errorf("query = %q", gotQuery.Get("query"))
	}
	if gotQuery.Get("start") != "1000" || gotQuery.Get("end") != "4600" || gotQuery.Get("step") != "300" {
		t.Errorf("range params = %v", gotQuery)
	}
	if gotOrg != "156331" {
		t.Errorf("X-Scope-OrgID = %q", gotOrg)
	}
	if gotCache != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", gotCache)
	}
	if gotUA != "querybench" {
		t.Errorf("User-Agent = %q, want querybench", gotUA)
	}
	if res.ProcessedBytes != 123456 {
		t.Errorf("ProcessedBytes = %d, want 123456", res.ProcessedBytes)
	}
}

func TestRun_InstantRequest(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Write([]byte(statsBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "t", Options{})
	q := queries.Query{Name: "i", Type: queries.TypeInstant, Expr: "sum(y)"}
	end := time.Unix(4600, 0)

	if _, err := c.Run(context.Background(), q, end.Add(-time.Hour), end); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotPath != "/loki/api/v1/query" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery.Get("time") != "4600" {
		t.Errorf("time = %q, want 4600", gotQuery.Get("time"))
	}
	if gotQuery.Has("step") {
		t.Errorf("instant query must not send step, got %q", gotQuery.Get("step"))
	}
}

func TestRun_NonOKIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "t", Options{})
	q := queries.Query{Name: "i", Type: queries.TypeInstant, Expr: "x"}
	res, err := c.Run(context.Background(), q, time.Unix(0, 0), time.Unix(1, 0))
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", res.StatusCode)
	}
}

func TestRun_AbsentStatsIsZeroWithoutLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"result":[]}}`)) // 200, no stats object
	}))
	defer srv.Close()

	var logs int
	c := New(srv.URL, "t", Options{Logf: func(string, ...any) { logs++ }})
	q := queries.Query{Name: "i", Type: queries.TypeInstant, Expr: "x"}
	res, err := c.Run(context.Background(), q, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ProcessedBytes != 0 {
		t.Errorf("ProcessedBytes = %d, want 0 for absent stats", res.ProcessedBytes)
	}
	if logs != 0 {
		t.Errorf("absent stats must not log a parse warning, got %d", logs)
	}
}

func TestRun_UnparseableStatsLogsAndZeroes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not valid json`)) // 200 but malformed
	}))
	defer srv.Close()

	var logs int
	c := New(srv.URL, "t", Options{Logf: func(string, ...any) { logs++ }})
	q := queries.Query{Name: "i", Type: queries.TypeInstant, Expr: "x"}
	res, err := c.Run(context.Background(), q, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("Run should succeed on a 200 even with unparseable stats: %v", err)
	}
	if res.ProcessedBytes != 0 {
		t.Errorf("ProcessedBytes = %d, want 0", res.ProcessedBytes)
	}
	if logs != 1 {
		t.Errorf("unparseable stats must log exactly one warning, got %d", logs)
	}
}
