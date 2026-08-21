package metrics

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/grafana/loki-query-benchmark/internal/report"
)

// Capturer captures the backend system metrics for a query's run window from one
// Prometheus datasource, scoped to one namespace.
type Capturer struct {
	datasource string
	namespace  string
	run        Runner
	logf       func(format string, args ...any)
}

// Options configure a Capturer.
type Options struct {
	// Datasource is the gcx Prometheus datasource UID the metric queries run
	// against.
	Datasource string
	// Namespace is the backend namespace whose metrics are captured; it fills the
	// namespace and job label selectors.
	Namespace string
	// Runner overrides how gcx is executed. The real gcx CLI is used when nil.
	Runner Runner
	// Logf receives one warning per metric that could not be captured. It may be
	// nil.
	Logf func(format string, args ...any)
}

// New returns a Capturer.
func New(opts Options) *Capturer {
	run := opts.Runner
	if run == nil {
		run = execRunner
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Capturer{
		datasource: opts.Datasource,
		namespace:  opts.Namespace,
		run:        run,
		logf:       logf,
	}
}

// metric defines one captured value: the PromQL to run, how to turn the raw
// sample into the reported value, and where to store it.
type metric struct {
	name     string
	expr     func(ns, window string) string
	finalize func(raw float64, runDuration time.Duration) float64
	set      func(m *report.SystemStats, v float64)
}

// asIs returns the raw sample unchanged.
func asIs(raw float64, _ time.Duration) float64 { return raw }

// perSecond divides the raw sample by the run duration, turning a windowed total
// into an average per-second rate (CPU cores, allocation bytes per second).
func perSecond(raw float64, runDuration time.Duration) float64 {
	secs := runDuration.Seconds()
	if secs <= 0 {
		return raw
	}
	return raw / secs
}

func fptr(v float64) *float64 { return &v }

// uptr rounds v to the nearest whole unit for a counter or byte total, clamping
// the negatives that increase() should never produce to zero.
func uptr(v float64) *uint64 {
	if v < 0 {
		v = 0
	}
	u := uint64(math.Round(v))
	return &u
}

// metricDefs is the fixed set of backend metrics captured per query window. The
// window placeholder is filled with the padded run window; the CPU and
// allocation metrics divide by the un-padded run duration.
var metricDefs = []metric{
	{
		name: "querier_objstore_requests",
		expr: func(ns, w string) string {
			return `sum(increase(loki_objstore_bucket_operations_total{namespace="` + ns + `", container="querier"}[` + w + `]))`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.ObjstoreRequests = uptr(v) },
	},
	{
		name: "querier_objstore_fetched_bytes",
		expr: func(ns, w string) string {
			return `sum(increase(loki_objstore_bucket_operation_fetched_bytes_total{namespace="` + ns + `", container="querier"}[` + w + `]))`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.ObjstoreFetchedBytes = uptr(v) },
	},
	{
		name: "querier_cpu_seconds",
		expr: func(ns, w string) string {
			return `sum(increase(container_cpu_usage_seconds_total{namespace=~"` + ns + `", container=~"querier"}[` + w + `]))`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.CPUSeconds = fptr(v) },
	},
	{
		// Peak concurrent querier CPU: the summed per-pod CPU rate, sampled across
		// the window, at its maximum. Immune to the padding window's idle time,
		// since idle never exceeds the active peak.
		name: "querier_cpu_peak_cores",
		expr: func(ns, w string) string {
			return `max_over_time(sum(irate(container_cpu_usage_seconds_total{namespace=~"` + ns + `", container=~"querier"}[1m]))[` + w + `:10s])`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.CPUPeakCores = fptr(v) },
	},
	{
		name: "querier_heap_inuse_peak_bytes",
		expr: func(ns, w string) string {
			return `max(max_over_time(go_memstats_heap_inuse_bytes{job=~"` + ns + `/querier.*"}[` + w + `]))`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.HeapInusePeakBytes = uptr(v) },
	},
	{
		name: "querier_alloc_bytes_per_second",
		expr: func(ns, w string) string {
			return `sum(increase(go_memstats_alloc_bytes_total{namespace="` + ns + `", container="querier"}[` + w + `]))`
		},
		finalize: perSecond,
		set:      func(m *report.SystemStats, v float64) { m.AllocBytesPerSecond = uptr(v) },
	},
	{
		name: "memcached_written_bytes",
		expr: func(ns, w string) string {
			return `sum(increase(memcached_written_bytes_total{job=~"` + ns + `/(memcached|memcached-extstore)"}[` + w + `]))`
		},
		finalize: asIs,
		set:      func(m *report.SystemStats, v float64) { m.MemcachedWrittenBytes = uptr(v) },
	},
}

// Capture reads every backend metric for one query's run window.
//
// metricsScrapeTime is the instant the queries are evaluated at (the run end
// plus the scrape-delay padding). window is the range-vector span covering the
// padded run window. runDuration is the un-padded wall-clock length of the run,
// used as the denominator for the CPU and allocation rates.
//
// A metric that gcx cannot return is left nil and logged; one failed metric
// never fails the others.
func (c *Capturer) Capture(ctx context.Context, metricsScrapeTime time.Time, window, runDuration time.Duration) report.SystemStats {
	w := promDuration(window)
	var m report.SystemStats
	for _, def := range metricDefs {
		// A cancelled context would fail every remaining metric the same way; stop
		// rather than log one warning per metric.
		if ctx.Err() != nil {
			return m
		}
		expr := def.expr(c.namespace, w)
		raw, err := queryInstant(ctx, c.run, c.datasource, expr, metricsScrapeTime)
		if err != nil {
			var noData errNoData
			if errors.As(err, &noData) {
				c.logf("metric %s: no sample returned", def.name)
			} else {
				c.logf("metric %s: %v", def.name, err)
			}
			continue
		}
		def.set(&m, def.finalize(raw, runDuration))
	}
	return m
}
