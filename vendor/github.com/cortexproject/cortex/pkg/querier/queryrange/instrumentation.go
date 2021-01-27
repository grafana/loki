package queryrange

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
)

// InstrumentMiddleware can be inserted into the middleware chain to expose timing information.
func InstrumentMiddleware(name string, metrics *InstrumentMiddlewareMetrics) Middleware {
	var durationCol instrument.Collector

	// Support the case metrics shouldn't be tracked (ie. unit tests).
	if metrics != nil {
		durationCol = instrument.NewHistogramCollector(metrics.duration)
	} else {
		durationCol = &NoopCollector{}
	}

	return MiddlewareFunc(func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, req Request) (Response, error) {
			var resp Response
			err := instrument.CollectedRequest(ctx, name, durationCol, instrument.ErrorCode, func(ctx context.Context) error {
				var err error
				resp, err = next.Do(ctx, req)
				return err
			})
			return resp, err
		})
	})
}

// InstrumentMiddlewareMetrics holds the metrics tracked by InstrumentMiddleware.
type InstrumentMiddlewareMetrics struct {
	duration *prometheus.HistogramVec
}

// NewInstrumentMiddlewareMetrics makes a new InstrumentMiddlewareMetrics.
func NewInstrumentMiddlewareMetrics(registerer prometheus.Registerer) *InstrumentMiddlewareMetrics {
	return &InstrumentMiddlewareMetrics{
		duration: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "frontend_query_range_duration_seconds",
			Help:      "Total time spent in seconds doing query range requests.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "status_code"}),
	}
}

// NoopCollector is a noop collector that can be used as placeholder when no metric
// should tracked by the instrumentation.
type NoopCollector struct{}

// Register implements instrument.Collector.
func (c *NoopCollector) Register() {}

// Before implements instrument.Collector.
func (c *NoopCollector) Before(method string, start time.Time) {}

// After implements instrument.Collector.
func (c *NoopCollector) After(method, statusCode string, start time.Time) {}
