package metrics

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeRunner dispatches on the metric name inside the PromQL expression, so the
// test controls each metric's raw sample without launching gcx.
type fakeRunner struct {
	raw      map[string]float64 // metric substring -> raw sample
	failSub  string             // metric substring whose query returns an error
	seenExpr []string
}

func (f *fakeRunner) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	expr := findExpr(args)
	f.seenExpr = append(f.seenExpr, expr)
	if f.failSub != "" && strings.Contains(expr, f.failSub) {
		return nil, fmt.Errorf("boom")
	}
	for sub, v := range f.raw {
		if strings.Contains(expr, sub) {
			return []byte(fmt.Sprintf(`{"status":"success","data":{"result":[{"value":[1,"%g"]}]}}`, v)), nil
		}
	}
	return []byte(`{"status":"success","data":{"result":[]}}`), nil
}

// findExpr returns the PromQL expression argument (the one containing a metric
// selector) from a gcx argument list.
func findExpr(args []string) string {
	for _, a := range args {
		if strings.Contains(a, "{") {
			return a
		}
	}
	return ""
}

func TestCapture(t *testing.T) {
	fr := &fakeRunner{raw: map[string]float64{
		"loki_objstore_bucket_operations_total":              50,
		"loki_objstore_bucket_operation_fetched_bytes_total": 4000,
		"increase(container_cpu_usage_seconds_total":         200, // cpu_seconds (asIs)
		"irate(container_cpu_usage_seconds_total":            5,   // cpu_peak_cores (asIs)
		"go_memstats_heap_inuse_bytes":                       8e8,
		"go_memstats_alloc_bytes_total":                      1000, // /100s -> 10 B/s
		"memcached_written_bytes_total":                      7000,
	}}
	c := New(Options{Datasource: "ds", Namespace: "loki-dev-002", Runner: fr.run})

	metricsScrapeTime := time.Date(2026, 8, 20, 14, 17, 0, 0, time.UTC)
	m := c.Capture(context.Background(), metricsScrapeTime, 14*time.Minute, 100*time.Second)

	if m.ObjstoreRequests == nil || *m.ObjstoreRequests != 50 {
		t.Errorf("ObjstoreRequests = %v, want 50", m.ObjstoreRequests)
	}
	if m.ObjstoreFetchedBytes == nil || *m.ObjstoreFetchedBytes != 4000 {
		t.Errorf("ObjstoreFetchedBytes = %v, want 4000", m.ObjstoreFetchedBytes)
	}
	if m.CPUSeconds == nil || *m.CPUSeconds != 200 {
		t.Errorf("CPUSeconds = %v, want 200 (raw increase)", m.CPUSeconds)
	}
	if m.CPUPeakCores == nil || *m.CPUPeakCores != 5 {
		t.Errorf("CPUPeakCores = %v, want 5 (raw peak)", m.CPUPeakCores)
	}
	if m.HeapInusePeakBytes == nil || *m.HeapInusePeakBytes != 8e8 {
		t.Errorf("HeapInusePeakBytes = %v, want 8e8", m.HeapInusePeakBytes)
	}
	if m.AllocBytesPerSecond == nil || *m.AllocBytesPerSecond != 10 {
		t.Errorf("AllocBytesPerSecond = %v, want 10 (1000/100s)", m.AllocBytesPerSecond)
	}
	if m.MemcachedWrittenBytes == nil || *m.MemcachedWrittenBytes != 7000 {
		t.Errorf("MemcachedWrittenBytes = %v, want 7000", m.MemcachedWrittenBytes)
	}

	// The window must reach every expr as whole seconds, and the namespace must be
	// substituted.
	joined := strings.Join(fr.seenExpr, "\n")
	if !strings.Contains(joined, "[840s]") {
		t.Errorf("expected [840s] window in exprs:\n%s", joined)
	}
	if !strings.Contains(joined, `namespace="loki-dev-002"`) {
		t.Errorf("expected namespace substitution in exprs:\n%s", joined)
	}
}

func TestCapture_FailedMetricIsNil(t *testing.T) {
	fr := &fakeRunner{
		raw:     map[string]float64{"loki_objstore_bucket_operations_total": 50},
		failSub: "memcached_written_bytes_total",
	}
	c := New(Options{Datasource: "ds", Namespace: "ns", Runner: fr.run})

	m := c.Capture(context.Background(), time.Now(), time.Minute, time.Minute)
	if m.MemcachedWrittenBytes != nil {
		t.Errorf("failed metric should be nil, got %v", *m.MemcachedWrittenBytes)
	}
	if m.ObjstoreRequests == nil || *m.ObjstoreRequests != 50 {
		t.Errorf("a failing metric must not suppress the others: got %v", m.ObjstoreRequests)
	}
}
