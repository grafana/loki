// Package report defines the on-disk benchmark report and its I/O.
//
// A Report is the record of one querybench run: the fixed run parameters plus,
// per query, the exact request, the per-run latencies, the response statistics,
// and the backend system metrics captured over the run window. It is written as
// indented JSON so it stays diffable and human-readable.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/queries"
)

// Report is the top-level benchmark report for one querybench run.
//
// The tool rewrites the whole Report to disk after each query completes, so a
// crash mid-run leaves the queries finished so far intact. FinishedAt is nil
// until the run ends.
type Report struct {
	Description      string     `json:"description"`
	LokiURL          string     `json:"loki_url"`
	Tenant           string     `json:"tenant"`
	BackendNamespace string     `json:"backend_namespace"`
	RequestedStart   time.Time  `json:"requested_start"`
	RequestedEnd     time.Time  `json:"requested_end"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	Queries          []Query    `json:"queries"`
}

// Query is the outcome of running one benchmark query Runs times back-to-back.
//
// Start and End are the query's data time range: End is the shared anchor, and
// Start is End minus the query's window and the longest range vector in Expr. So
// End-Start is the true span of data read — a range query's query_range start is
// later than Start, by the range vector — and it stays comparable across both
// query types. StepSeconds is zero for instant queries.
type Query struct {
	Name        string            `json:"name"`
	Type        queries.QueryType `json:"type"`
	Expr        string            `json:"expr"`
	Start       time.Time         `json:"start"`
	End         time.Time         `json:"end"`
	StepSeconds float64           `json:"step_seconds"`
	Runs        int               `json:"runs"`

	// ExecutionStartedAt and ExecutionFinishedAt bound the real-world wall-clock
	// window in which the Runs executions ran. The system metrics are captured
	// over this window (padded on both sides), and the CPU and allocation rates
	// use its length as their denominator.
	ExecutionStartedAt  time.Time `json:"execution_started_at"`
	ExecutionFinishedAt time.Time `json:"execution_finished_at"`

	// LatenciesSeconds holds one entry per successful execution, in seconds.
	LatenciesSeconds []float64 `json:"latencies_seconds"`
	// FailedRuns counts executions that returned an error or a non-200 status.
	// Those executions contribute no latency and no processed bytes.
	FailedRuns int `json:"failed_runs"`

	QueryStats    QueryStats  `json:"query_stats"`
	SystemMetrics SystemStats `json:"system_metrics"`
}

// QueryStats holds totals extracted from the query responses, summed over all
// runs.
type QueryStats struct {
	ProcessedBytes int64 `json:"processed_bytes"`
}

// SystemStats holds the backend metrics captured for one query's run window.
//
// Each field is a pointer so a metric that could not be captured (gcx error or
// empty result) serializes as JSON null rather than a misleading zero. Counts
// and byte totals are rounded to whole units; the CPU fields keep their
// fractional values. ObjstoreRequests, the byte totals and CPUSeconds are totals
// over the whole window; AllocBytesPerSecond is a total divided by the run
// duration (a rate); HeapInusePeakBytes and CPUPeakCores are peaks over the
// window.
type SystemStats struct {
	ObjstoreRequests      *uint64  `json:"objstore_requests"`
	ObjstoreFetchedBytes  *uint64  `json:"objstore_fetched_bytes"`
	CPUSeconds            *float64 `json:"cpu_seconds"`
	CPUPeakCores          *float64 `json:"cpu_peak_cores"`
	HeapInusePeakBytes    *uint64  `json:"heap_inuse_peak_bytes"`
	AllocBytesPerSecond   *uint64  `json:"alloc_bytes_per_second"`
	MemcachedWrittenBytes *uint64  `json:"memcached_written_bytes"`
}

// Filename returns the report filename for a run started at t, in the tool's
// fixed format "YYYY-MM-DD-at-HH-MM.json" using t's own location.
func Filename(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d-at-%02d-%02d.json",
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
}

// Create reserves the report path dir/Filename(startedAt) and returns it.
//
// It fails if the file already exists, so a run never overwrites an earlier
// report. The directory is created if missing.
func Create(dir string, startedAt time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir %q: %w", dir, err)
	}
	path := filepath.Join(dir, Filename(startedAt))
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("report %q already exists", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat report %q: %w", path, err)
	}
	return path, nil
}

// Write serializes r to path atomically: it writes a temporary file in the same
// directory and renames it over path, so a process crash mid-write never
// truncates an earlier good report. The temp file is not fsync'd, so surviving
// power loss is out of scope.
//
// Timestamps serialize in whatever zone they carry. The tool runs with
// time.Local set to UTC (see main), so every timestamp it produces is UTC.
func Write(path string, r *Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".querybench-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp report: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp report: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename report into place: %w", err)
	}
	return nil
}

// Load reads and parses the report at path. Every timestamp is converted to UTC
// (the instant is preserved; only the zone changes), so a report written with a
// different offset still loads with uniform UTC timestamps.
func Load(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report %q: %w", path, err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse report %q: %w", path, err)
	}

	r.RequestedStart = r.RequestedStart.UTC()
	r.RequestedEnd = r.RequestedEnd.UTC()
	r.StartedAt = r.StartedAt.UTC()
	if r.FinishedAt != nil {
		finished := r.FinishedAt.UTC()
		r.FinishedAt = &finished
	}
	for i := range r.Queries {
		r.Queries[i].Start = r.Queries[i].Start.UTC()
		r.Queries[i].End = r.Queries[i].End.UTC()
		r.Queries[i].ExecutionStartedAt = r.Queries[i].ExecutionStartedAt.UTC()
		r.Queries[i].ExecutionFinishedAt = r.Queries[i].ExecutionFinishedAt.UTC()
	}
	return &r, nil
}
