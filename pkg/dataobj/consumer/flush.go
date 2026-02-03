package consumer

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

const (
	flushReasonMaxAge      = "max_age"
	flushReasonBuilderFull = "builder_full"
	flushReasonIdle        = "idle"
)

// A sorter allows mocking of [logsobj.Sorter] in tests.
type sorter interface {
	Sort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// An uploader allows mocking of [uploader.Uploader] in tests.
type uploader interface {
	Upload(ctx context.Context, obj *dataobj.Object) (string, error)
}

// A flushJob contains all information needed to flush a data object builder.
type flushJob struct {
	builder
	// done is called when the job has finished.
	done func(flushJobResult)
}

// A flushJobResult contains the result of a flush. The flush failed if err
// is non-nil.
type flushJobResult struct {
	objectPath string
	err        error
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

	// Used in tests.
	flushFunc func(context.Context, flushJob) (string, error)
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
	f.flushFunc = f.flush
	return f
}

// Flush flushes the data object builder. It returns an error if the flush fails.
func (f *flusherImpl) Flush(ctx context.Context, builder builder, reason string) (string, error) {
	var (
		res  flushJobResult
		done = make(chan struct{})
	)
	f.FlushAsync(ctx, builder, reason, func(doneRes flushJobResult) {
		res = doneRes
		close(done)
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-done:
		return res.objectPath, res.err
	}
}

// FlushAsync asynchronously flushes the data object builder and calls done
// when finished.
func (f *flusherImpl) FlushAsync(ctx context.Context, builder builder, reason string, done func(flushJobResult)) {
	f.flushes.WithLabelValues(reason).Inc()
	go f.doJob(ctx, flushJob{builder: builder, done: done})
}

func (f *flusherImpl) doJob(ctx context.Context, job flushJob) {
	timer := prometheus.NewTimer(f.flushDuration)
	defer timer.ObserveDuration()
	objectPath, err := f.flushFunc(ctx, job)
	if err != nil {
		f.flushFailures.Inc()
		job.done(flushJobResult{err: err})
		return
	}
	job.done(flushJobResult{objectPath: objectPath})
}

// flush builds a complete data object from the builder, uploads it, records
// it in the metastore, and emits an object written event to the events topic.
// It can be overidden in tests by replacing [jobFunc].
func (f *flusherImpl) flush(ctx context.Context, job flushJob) (string, error) {
	obj, closer, err := job.Flush()
	if err != nil {
		return "", fmt.Errorf("failed to flush data object builder: %w", err)
	}
	defer closer.Close()
	obj, closer, err = f.sorter.Sort(ctx, obj)
	if err != nil {
		return "", fmt.Errorf("failed to sort data object: %w", err)
	}
	defer closer.Close()
	objectPath, err := f.uploader.Upload(ctx, obj)
	if err != nil {
		return "", fmt.Errorf("failed to upload object: %w", err)
	}
	return objectPath, nil
}
