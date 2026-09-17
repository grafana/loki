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
	flushes       *prometheus.CounterVec
	flushFailures prometheus.Counter
	flushDuration prometheus.Histogram
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
// once it is closed. Failures to release scratch storage are logged rather than
// returned, so that cleanup cannot fail an already-uploaded object.
func (f *flusherImpl) flush(ctx context.Context, builder builder) (*dataobj.Object, io.Closer, string, error) {
	unsortedObj, unsortedObjCloser, err := builder.Flush()
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to flush data object builder: %w", err)
	}
	// Sort copies unsortedObj into a new object, so the unsorted object is no
	// longer needed once flush returns, whatever the outcome.
	defer func() {
		if err := unsortedObjCloser.Close(); err != nil {
			level.Warn(f.logger).Log("msg", "failed to release unsorted data object", "err", err)
		}
	}()

	sortedObj, sortedObjCloser, err := f.sorter.Sort(ctx, unsortedObj)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to sort data object: %w", err)
	}

	objectPath, err := f.uploader.Upload(ctx, sortedObj)
	if err != nil {
		if closeErr := sortedObjCloser.Close(); closeErr != nil {
			level.Warn(f.logger).Log("msg", "failed to release sorted data object", "err", closeErr)
		}
		return nil, nil, "", fmt.Errorf("failed to upload object: %w", err)
	}

	// Ownership of sortedObjCloser transfers to the caller.
	return sortedObj, sortedObjCloser, objectPath, nil
}
