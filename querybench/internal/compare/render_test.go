package compare

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/report"
)

func f(v float64) *float64 { return &v }
func u(v uint64) *uint64   { return &v }

// rangeQuery builds a range query result for the tests. end fixes the absolute
// window position, which must not affect matching across reports.
func rangeQuery(expr string, end time.Time, window, step time.Duration, runs int, lats []float64, processed int64, sys report.SystemStats) report.Query {
	return report.Query{
		Name:             expr,
		Type:             "range",
		Expr:             expr,
		Start:            end.Add(-window),
		End:              end,
		StepSeconds:      step.Seconds(),
		Runs:             runs,
		LatenciesSeconds: lats,
		QueryStats:       report.QueryStats{ProcessedBytes: processed},
		SystemMetrics:    sys,
	}
}

// instantQuery builds an instant query result for the tests.
func instantQuery(expr string, end time.Time, window time.Duration, runs int, lats []float64, processed int64, sys report.SystemStats) report.Query {
	return report.Query{
		Name:             expr,
		Type:             "instant",
		Expr:             expr,
		Start:            end.Add(-window),
		End:              end,
		StepSeconds:      0,
		Runs:             runs,
		LatenciesSeconds: lats,
		QueryStats:       report.QueryStats{ProcessedBytes: processed},
		SystemMetrics:    sys,
	}
}

// TestRender covers the whole markdown in one exact-output assertion. The
// scenario exercises: an instant query (no step), a range query matched across
// reports despite different absolute times, a query only in a, and a query only
// in b, plus per-query normalization over differing run counts (a: 2 runs, b: 4)
// and missing metrics rendered as a dash.
func TestRender(t *testing.T) {
	day1 := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	// full builds captured system stats. cpuSec is the run-window CPU-seconds
	// total (÷ runs in the table); cpuPeak is the peak cores (as-is).
	full := func(req, fetched, mem uint64, cpuSec, cpuPeak float64, heap, alloc uint64) report.SystemStats {
		return report.SystemStats{
			ObjstoreRequests: u(req), ObjstoreFetchedBytes: u(fetched), MemcachedWrittenBytes: u(mem),
			CPUSeconds: f(cpuSec), CPUPeakCores: f(cpuPeak), HeapInusePeakBytes: u(heap), AllocBytesPerSecond: u(alloc),
		}
	}

	a := &report.Report{
		Description:    "chunks backend",
		RequestedStart: day1.Add(-24 * time.Hour), RequestedEnd: day1,
		Queries: []report.Query{
			instantQuery("avg(i)", day1, time.Hour, 2, []float64{0.5, 0.7}, 2000,
				full(200, 4000, 6000, 4, 2, 800_000_000, 1000)),
			rangeQuery("sum(r)", day1, 6*time.Hour, 5*time.Minute, 2, []float64{1, 3}, 2000,
				full(100, 4000, 6000, 4, 2, 800_000_000, 1000)),
			rangeQuery("sum(a)", day1, time.Hour, time.Minute, 2, []float64{1, 1}, 10,
				report.SystemStats{CPUPeakCores: f(1)}),
		},
	}
	b := &report.Report{
		Description:    "dataobj backend",
		RequestedStart: day2.Add(-24 * time.Hour), RequestedEnd: day2,
		Queries: []report.Query{
			// Same expr/window/step as a's queries but a different day: they must match.
			instantQuery("avg(i)", day2, time.Hour, 4, []float64{1, 1, 1, 1}, 8000,
				full(800, 8000, 8000, 12, 3, 400_000_000, 2000)),
			rangeQuery("sum(r)", day2, 6*time.Hour, 5*time.Minute, 4, []float64{2, 2, 2, 2}, 8000,
				full(400, 8000, 8000, 12, 3, 400_000_000, 2000)),
			rangeQuery("sum(b)", day2, 2*time.Hour, 5*time.Minute, 4, []float64{5, 5, 5, 5}, 40,
				report.SystemStats{CPUPeakCores: f(4)}),
		},
	}

	got := Render(Input{"chunks", a}, Input{"dataobj", b})

	want := "# Query benchmark comparison\n" +
		"\n" +
		"Comparing **chunks** (a) vs **dataobj** (b).\n" +
		"\n" +
		"- **chunks** — `a`: 2 runs/query, 2026-08-18T00:00:00Z .. 2026-08-19T00:00:00Z — chunks backend\n" +
		"- **dataobj** — `b`: 4 runs/query, 2026-08-19T00:00:00Z .. 2026-08-20T00:00:00Z — dataobj backend\n" +
		"\n" +
		"| Query type | Query expression | Query steps | Min latency | 50p latency | Max latency | Processed bytes | Fetched bytes (object storage) | Fetched bytes (memcached) | Object storage requests | Querier CPU (s/query) | Querier peak CPU (cores) | Querier mem peak | Querier mem alloc |\n" +
		"| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n" +
		"| instant | `avg(i)`<br>window: 1h | – | 500 ms / 1.00 s (+100.0%) | 500 ms / 1.00 s (+100.0%) | 700 ms / 1.00 s (+42.9%) | 1 KB / 2 KB (+100.0%) | 2 KB / 2 KB (+0.0%) | 3 KB / 2 KB (-33.3%) | 100 / 200 (+100.0%) | 2.00 s / 3.00 s (+50.0%) | 2.00 / 3.00 (+50.0%) | 800 MB / 400 MB (-50.0%) | 1 KB/s / 2 KB/s (+100.0%) |\n" +
		"| range | `sum(r)`<br>window: 6h | 5m | 1.00 s / 2.00 s (+100.0%) | 1.00 s / 2.00 s (+100.0%) | 3.00 s / 2.00 s (-33.3%) | 1 KB / 2 KB (+100.0%) | 2 KB / 2 KB (+0.0%) | 3 KB / 2 KB (-33.3%) | 50 / 100 (+100.0%) | 2.00 s / 3.00 s (+50.0%) | 2.00 / 3.00 (+50.0%) | 800 MB / 400 MB (-50.0%) | 1 KB/s / 2 KB/s (+100.0%) |\n" +
		"| range | `sum(a)`<br>window: 1h | 1m | 1.00 s / – | 1.00 s / – | 1.00 s / – | 5 B / – | – / – | – / – | – / – | – / – | 1.00 / – | – / – | – / – |\n" +
		"| range | `sum(b)`<br>window: 2h | 5m | – / 5.00 s | – / 5.00 s | – / 5.00 s | – / 10 B | – / – | – / – | – / – | – / – | – / 4.00 | – / – | – / – |\n" +
		"\n" +
		"_Each cell is `a / b (±% of b vs a)`. All figures are per single query._ Latency min/50p/max come from the per-run latencies. Processed bytes come from the query responses; fetched bytes, object-storage requests and CPU seconds come from the metrics window; all are summed and divided by the run count. Querier peak CPU, memory peak and allocation rate are peaks or rates, already independent of the run count, so they are shown as captured. A `–` marks a query absent from one report or a metric that could not be captured; the percentage is omitted when either side is missing or the `a` value is zero.\n"

	if got != want {
		t.Fatalf("Render output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestPercentile(t *testing.T) {
	in := []float64{5, 1, 4, 2, 3}
	orig := slices.Clone(in)
	// Indices into the sorted [1 2 3 4 5]: p0→0, p50→(50*4)/100=2, p100→4.
	cases := map[int]float64{0: 1, 50: 3, 100: 5}
	for p, want := range cases {
		if got := percentile(in, p); got != want {
			t.Errorf("percentile(p=%d) = %v, want %v", p, got, want)
		}
	}
	if !slices.Equal(in, orig) {
		t.Errorf("percentile mutated its input: got %v, want %v", in, orig)
	}
}

func TestMdEscape_EscapesPipesAndNewlines(t *testing.T) {
	got := mdEscape(`{app="x"} |= "err"` + "\nsecond")
	if !strings.Contains(got, `\|`) {
		t.Errorf("pipe not escaped: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("newline not stripped: %q", got)
	}
}

func TestRender_ZeroBaselineOmitsPercent(t *testing.T) {
	end := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	mk := func(processed int64) *report.Report {
		return &report.Report{Queries: []report.Query{
			instantQuery("avg(z)", end, time.Hour, 1, []float64{1}, processed, report.SystemStats{}),
		}}
	}
	md := Render(Input{"a", mk(0)}, Input{"b", mk(100)})
	if !strings.Contains(md, "0 B / 100 B") {
		t.Fatalf("expected zero-baseline processed-bytes cell:\n%s", md)
	}
	if strings.Contains(md, "0 B / 100 B (") {
		t.Errorf("percentage must be omitted when the a value is zero:\n%s", md)
	}
}
