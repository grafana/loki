package instrument

import (
	"context"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	oldcontext "golang.org/x/net/context"

	"github.com/weaveworks/common/grpc"
	"github.com/weaveworks/common/user"
)

// DefBuckets are histogram buckets for the response time (in seconds)
// of a network service, including one that is responding very slowly.
var DefBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 25, 50, 100}

// Collector describes something that collects data before and/or after a task.
type Collector interface {
	Register()
	Before(method string, start time.Time)
	After(method, statusCode string, start time.Time)
}

// HistogramCollector collects the duration of a request
type HistogramCollector struct {
	metric *prometheus.HistogramVec
}

// HistogramCollectorBuckets define the buckets when passing the metric
var HistogramCollectorBuckets = []string{"operation", "status_code"}

// NewHistogramCollectorFromOpts creates a Collector from histogram options.
// It makes sure that the buckets are named properly and should be preferred over
// NewHistogramCollector().
func NewHistogramCollectorFromOpts(opts prometheus.HistogramOpts) *HistogramCollector {
	metric := prometheus.NewHistogramVec(opts, HistogramCollectorBuckets)
	return &HistogramCollector{metric}
}

// NewHistogramCollector creates a Collector from a metric.
func NewHistogramCollector(metric *prometheus.HistogramVec) *HistogramCollector {
	return &HistogramCollector{metric}
}

// Register registers metrics.
func (c *HistogramCollector) Register() {
	prometheus.MustRegister(c.metric)
}

// Before collects for the upcoming request.
func (c *HistogramCollector) Before(method string, start time.Time) {
}

// After collects when the request is done.
func (c *HistogramCollector) After(method, statusCode string, start time.Time) {
	if c.metric != nil {
		c.metric.WithLabelValues(method, statusCode).Observe(time.Now().Sub(start).Seconds())
	}
}

// JobCollector collects metrics for jobs. Designed for batch jobs which run on a regular,
// not-too-frequent, non-overlapping interval. We can afford to measure duration directly
// with gauges, and compute quantile with quantile_over_time.
type JobCollector struct {
	start, end, duration *prometheus.GaugeVec
	started, completed   *prometheus.CounterVec
}

// NewJobCollector instantiates JobCollector which creates its metrics.
func NewJobCollector(namespace string) *JobCollector {
	return &JobCollector{
		start: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "job",
			Name:      "latest_start_timestamp",
			Help:      "Unix UTC timestamp of most recent job start time",
		}, []string{"operation"}),
		end: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "job",
			Name:      "latest_end_timestamp",
			Help:      "Unix UTC timestamp of most recent job end time",
		}, []string{"operation", "status_code"}),
		duration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "job",
			Name:      "latest_duration_seconds",
			Help:      "duration of most recent job",
		}, []string{"operation", "status_code"}),
		started: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "job",
			Name:      "started_total",
			Help:      "Number of jobs started",
		}, []string{"operation"}),
		completed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "job",
			Name:      "completed_total",
			Help:      "Number of jobs completed",
		}, []string{"operation", "status_code"}),
	}
}

// Register registers metrics.
func (c *JobCollector) Register() {
	prometheus.MustRegister(c.start)
	prometheus.MustRegister(c.end)
	prometheus.MustRegister(c.duration)
	prometheus.MustRegister(c.started)
	prometheus.MustRegister(c.completed)
}

// Before collects for the upcoming request.
func (c *JobCollector) Before(method string, start time.Time) {
	c.start.WithLabelValues(method).Set(float64(start.UTC().Unix()))
	c.started.WithLabelValues(method).Inc()
}

// After collects when the request is done.
func (c *JobCollector) After(method, statusCode string, start time.Time) {
	end := time.Now()
	c.end.WithLabelValues(method, statusCode).Set(float64(end.UTC().Unix()))
	c.duration.WithLabelValues(method, statusCode).Set(end.Sub(start).Seconds())
	c.completed.WithLabelValues(method, statusCode).Inc()
}

// CollectedRequest runs a tracked request. It uses the given Collector to monitor requests.
//
// If `f` returns no error we log "200" as status code, otherwise "500". Pass in a function
// for `toStatusCode` to overwrite this behaviour. It will also emit an OpenTracing span if
// you have a global tracer configured.
func CollectedRequest(ctx context.Context, method string, col Collector, toStatusCode func(error) string, f func(context.Context) error) error {
	if toStatusCode == nil {
		toStatusCode = ErrorCode
	}
	sp, newCtx := opentracing.StartSpanFromContext(ctx, method)
	ext.SpanKindRPCClient.Set(sp)
	if userID, err := user.ExtractUserID(ctx); err == nil {
		sp.SetTag("user", userID)
	}
	if orgID, err := user.ExtractOrgID(ctx); err == nil {
		sp.SetTag("organization", orgID)
	}

	start := time.Now()
	col.Before(method, start)
	err := f(newCtx)
	col.After(method, toStatusCode(err), start)

	if err != nil {
		if !grpc.IsCanceled(err) {
			ext.Error.Set(sp, true)
		}
		sp.LogFields(otlog.Error(err))
	}
	sp.Finish()

	return err
}

// ErrorCode converts an error into an HTTP status code
func ErrorCode(err error) string {
	if err == nil {
		return "200"
	}
	return "500"
}

// TimeRequestHistogram runs 'f' and records how long it took in the given Prometheus
// histogram metric. If 'f' returns successfully, record a "200". Otherwise, record
// "500".  It will also emit an OpenTracing span if you have a global tracer configured.
//
// Deprecated: Use CollectedRequest()
func TimeRequestHistogram(ctx oldcontext.Context, method string, metric *prometheus.HistogramVec, f func(context.Context) error) error {
	return CollectedRequest(ctx, method, NewHistogramCollector(metric), ErrorCode, f)
}

// TimeRequestHistogramStatus runs 'f' and records how long it took in the given Prometheus
// histogram metric. If 'f' returns successfully, record a "200". Otherwise, record
// "500".  It will also emit an OpenTracing span if you have a global tracer configured.
//
// Deprecated: Use CollectedRequest()
func TimeRequestHistogramStatus(ctx oldcontext.Context, method string, metric *prometheus.HistogramVec, toStatusCode func(error) string, f func(context.Context) error) error {
	return CollectedRequest(ctx, method, NewHistogramCollector(metric), toStatusCode, f)
}
