package consumer

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

const (
	flushReasonBuilderFull = "builder_full"
	flushReasonIdle        = "idle"
	flushReasonMaxAge      = "max_age"
)

// A sorter allows mocking of [logsobj.Sorter] in tests.
type sorter interface {
	Sort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// An uploader allows mocking of [uploader.Uploader] in tests.
type uploader interface {
	Upload(ctx context.Context, obj *dataobj.Object) (string, error)
}

// A flusherImpl is responsible for flushing data object builders to data objects.
type flusherImpl struct {
	sorter   sorter
	uploader uploader
	logger   log.Logger

	// Metrics.
	flushes         *prometheus.CounterVec
	flushFailures   prometheus.Counter
	flushDuration   prometheus.Histogram
	releaseFailures prometheus.Counter
}

func newFlusher(sorter sorter, uploader uploader, logger log.Logger, r prometheus.Registerer) *flusherImpl {
	f := &flusherImpl{
		sorter:   sorter,
		uploader: uploader,
		logger:   logger,
		flushes: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_flushes_total",
			Help: "Total number of flushes.",
		}, []string{"reason"}),
		flushFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_flush_failures_total",
			Help: "Total number of failed flushes.",
		}),
		releaseFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_release_failures_total",
			Help: "Total number of failures to release a flushed data object's scratch storage. The object is already uploaded at that point, so these do not fail the flush.",
		}),
		flushDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_consumer_flush_duration_seconds",
			Help: "Time taken to flush a data object.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}
	// Initialize each counter to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	f.flushes.WithLabelValues(flushReasonBuilderFull).Add(0)
	f.flushes.WithLabelValues(flushReasonIdle).Add(0)
	f.flushes.WithLabelValues(flushReasonMaxAge).Add(0)
	return f
}

// Flush flushes the data object builder. It returns an error if the flush fails.
func (f *flusherImpl) Flush(ctx context.Context, builder builder, reason string) (*dataobj.Object, io.Closer, string, error) {
	f.flushes.WithLabelValues(reason).Inc()
	timer := prometheus.NewTimer(f.flushDuration)
	defer timer.ObserveDuration()
	obj, objCloser, objPath, err := f.flush(ctx, builder)
	if err != nil {
		f.flushFailures.Inc()
	}
	return obj, objCloser, objPath, err
}

// flush builds a complete data object from the builder, sorts and uploads it.
// On success the caller owns the returned [io.Closer]; reads of the object fail
// once it is closed. If an error is returned the closer is always nil.
func (f *flusherImpl) flush(ctx context.Context, builder builder) (*dataobj.Object, io.Closer, string, error) {
	unsortedObj, unsortedObjCloser, err := builder.Flush()
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to flush data object builder: %w", err)
	}
	// Sort copies unsortedObj into a new object, so the unsorted object is no
	// longer needed once flush returns, whatever the outcome.
	defer f.release(unsortedObjCloser, "unsorted")

	sortedObj, sortedObjCloser, err := f.sorter.Sort(ctx, unsortedObj)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to sort data object: %w", err)
	}

	objectPath, err := f.uploader.Upload(ctx, sortedObj)
	if err != nil {
		f.release(sortedObjCloser, "sorted")
		return nil, nil, "", fmt.Errorf("failed to upload object: %w", err)
	}

	// Ownership of the sorted object transfers to the caller, but it is still
	// released through f.release so failures are counted in one place wherever
	// the object happens to be closed.
	return sortedObj, closerFunc(func() { f.release(sortedObjCloser, "flushed") }), objectPath, nil
}

// release closes a data object's backing scratch storage. Failures are counted
// and logged rather than returned: by the time an object is released it has
// already been uploaded, so a cleanup failure must not fail the flush.
func (f *flusherImpl) release(closer io.Closer, which string) {
	if err := closer.Close(); err != nil {
		f.releaseFailures.Inc()
		level.Warn(f.logger).Log("msg", "failed to release data object", "object", which, "err", err)
	}
}

// closerFunc adapts a release function to [io.Closer]. Close always reports
// success, because releasing scratch storage is cleanup that its caller cannot
// act on.
type closerFunc func()

func (f closerFunc) Close() error {
	f()
	return nil
}
