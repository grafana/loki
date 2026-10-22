package builder

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// A committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, partition int32, offset int64) error
}

// An indexer allows mocking of index building in tests. Implementations must
// be safe for concurrent use.
type indexer interface {
	Index(ctx context.Context, obj *dataobj.Object, objPath string) (index.Result, error)
}

// A tocWriter allows mocking of [metastore.TableOfContentsWriter] in tests.
// Implementations must be safe for concurrent use.
type tocWriter interface {
	WriteEntry(ctx context.Context, tenant string, entry metastore.TableOfContentsEntry) error
}

// A flusher allows mocking of flushes in tests. Implementations must be safe
// for concurrent use.
type flusher interface {
	// Flush builds, sorts and uploads the builder's data object. On success the
	// caller owns the returned [io.Closer] and must close it once it is done
	// reading the object; reads of the object fail after that. If an error is
	// returned the closer is always nil.
	Flush(ctx context.Context, builder builder, reason string) (*dataobj.Object, io.Closer, string, error)
}

// A flushCommitterImpl manages the flushing of data objects and commits.
type flushCommitterImpl struct {
	flusher   flusher
	committer committer
	indexer   indexer
	tocWriter tocWriter
	partition int32
	logger    log.Logger

	// concurrency is the maximum number of builders that one flush has in
	// progress. Each builder in progress holds its sorted object until the ToC
	// records its index, so concurrency also bounds the sorted objects in
	// memory. It is at least 1.
	concurrency int

	duration      *prometheus.HistogramVec
	e2eLagSeconds prometheus.Gauge
}

const (
	resultOK        = "ok"
	resultError     = "error"
	resultCancelled = "cancelled"
)

func newFlushCommitter(
	flusher flusher,
	committer committer,
	indexer indexer,
	tocWriter tocWriter,
	partition int32,
	concurrency int,
	logger log.Logger,
	r prometheus.Registerer,
) *flushCommitterImpl {
	d := promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
		Name:                            "loki_dataobj_builder_flush_commit_duration_seconds",
		Help:                            "Time taken to flush a group of builders and commit the Kafka offset.",
		Buckets:                         prometheus.DefBuckets,
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 0,
	}, []string{"result"})

	// Report the outcomes from the start, so that a rate over failures reads
	// as zero rather than going missing until the first one happens.
	d.WithLabelValues(resultOK)
	d.WithLabelValues(resultError)
	d.WithLabelValues(resultCancelled)

	return &flushCommitterImpl{
		flusher:     flusher,
		committer:   committer,
		indexer:     indexer,
		tocWriter:   tocWriter,
		partition:   partition,
		logger:      logger,
		duration:    d,
		concurrency: concurrency,
		e2eLagSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_e2e_lag_seconds",
			Help: "Time between a log line being written to Kafka by the distributors and it becoming available for querying in seconds.",
		}),
	}
}

// Flush multiple data object builders and, if successful, commit the offset.
func (c *flushCommitterImpl) Flush(ctx context.Context, builders []builder, reason string, offset int64) (returnErr error) {
	start := time.Now()
	defer func() { c.observeResult(time.Since(start), returnErr) }()

	// Read before flushing: flushing resets the builders, which clears their
	// earliest record times.
	earliest := earliestRecordTime(builders)

	if err := c.flushAll(ctx, builders, reason); err != nil {
		return err
	}

	// Everything above is queryable once its index is in the metastore, so the
	// oldest record in this flush is the one that determines ingest lag.
	if !earliest.IsZero() {
		c.e2eLagSeconds.Set(time.Since(earliest).Seconds())
	}

	// commit returns an error only if context is cancelled, otherwise it retries indefinitely
	if err := c.commit(ctx, offset); err != nil {
		return fmt.Errorf("failed to commit data object offset %d: %w", offset, err)
	}
	return nil
}

// flushAll flushes and indexes each builder and records each index in the
// ToC, with at most c.concurrency builders in progress. It returns once every
// started builder is done and has released its object.
//
// The first failure cancels the builders in progress and skips the builders
// not yet started. flushAll returns that first failure, not the cancellation
// errors that it causes in the other builders.
func (c *flushCommitterImpl) flushAll(ctx context.Context, builders []builder, reason string) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(c.concurrency)

	ordered := builders
	if c.concurrency > 1 {
		// One large builder can take most of the flush time. If it starts
		// last, it runs alone after the small builders. If it starts first,
		// the small builders run beside it, and the flush ends sooner.
		//
		// With one slot, the order does not change the flush time. Largest
		// first would only delay the ToC entries of the small builders.
		//
		// Largest first also builds the largest sorted objects at the same
		// time, which raises peak memory.
		ordered = largestFirst(builders)
	}
	for _, b := range ordered {
		g.Go(func() error {
			// Skip the builder if the flush was canceled or another builder
			// failed while this one waited for a slot. FlushAndSort ignores ctx
			// while it builds, so it would build the whole object before the
			// sort sees the cancellation.
			if err := gctx.Err(); err != nil {
				return err
			}
			return c.flushOne(gctx, b, reason)
		})
	}
	return g.Wait()
}

// largestFirst returns a new slice with the builders sorted by estimated size,
// largest first. Builders of equal size keep their order. largestFirst does
// not modify the given slice.
func largestFirst(builders []builder) []builder {
	ordered := slices.Clone(builders)
	slices.SortStableFunc(ordered, func(a, b builder) int {
		return cmp.Compare(b.GetEstimatedSize(), a.GetEstimatedSize())
	})
	return ordered
}

func (c *flushCommitterImpl) flushOne(ctx context.Context, builder builder, reason string) error {
	obj, objCloser, objPath, err := c.flusher.Flush(ctx, builder, reason)
	if err != nil {
		return fmt.Errorf("failed to flush data object: %w", err)
	}
	// Releasing the object never fails the flush: it is already uploaded, and
	// the flusher counts and logs any failure.
	defer func() { _ = objCloser.Close() }()

	// index returns an error if the context is canceled or the object can't
	// be processed. It retries every other error indefinitely.
	res, err := c.index(ctx, obj, objPath)
	if err != nil {
		return fmt.Errorf("failed to index data object: %w", err)
	}

	// A WriteEntry error fails the flush, so the offset is not committed and
	// the records are read again.
	if err := c.tocWriter.WriteEntry(ctx, res.TimeRange.Tenant, metastore.TableOfContentsEntry{
		Path:      res.Path,
		StartTime: res.TimeRange.MinTime,
		EndTime:   res.TimeRange.MaxTime,
	}); err != nil {
		return fmt.Errorf("failed to update metastore ToC: %w", err)
	}

	return nil
}

// earliestRecordTime returns the oldest record time across builders, or the
// zero time if none of them hold any records. It must be called before the
// builders are flushed, as flushing resets them.
func earliestRecordTime(builders []builder) time.Time {
	var earliest time.Time
	for _, b := range builders {
		t := b.GetEarliestRecordTime()
		if t.IsZero() {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

// index builds and uploads the index for the object, retrying with exponential
// backoff until successful or the context is canceled. Retrying is safe because
// the index is not referenced from the metastore until it is recorded in the
// ToC.
//
// An error that wraps [index.ErrUnprocessableObject] comes from the shape of
// the object. It is returned right away, as retrying can't fix it.
func (c *flushCommitterImpl) index(ctx context.Context, obj *dataobj.Object, objPath string) (index.Result, error) {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	var lastErr error
	for b.Ongoing() {
		res, err := c.indexer.Index(ctx, obj, objPath)
		if err == nil {
			return res, nil
		}
		if errors.Is(err, index.ErrUnprocessableObject) {
			return index.Result{}, err
		}
		lastErr = err
		level.Warn(c.logger).Log("msg", "failed to index data object", "err", lastErr, "attempt", b.NumRetries())
		b.Wait()
	}
	// The loop only gives up once the context is done. Surface that alongside
	// the last failure so the processor shuts down gracefully instead of
	// treating it as an unrecoverable flush.
	return index.Result{}, errors.Join(b.Err(), lastErr)
}

// commits the offset, retries with exponential backoff until successful or
// the context is canceled.
func (c *flushCommitterImpl) commit(ctx context.Context, offset int64) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	var lastErr error
	for b.Ongoing() {
		lastErr = c.committer.Commit(ctx, c.partition, offset)
		if lastErr == nil {
			level.Debug(c.logger).Log("msg", "committed offset", "partition", c.partition, "offset", offset)
			break
		}
		level.Warn(c.logger).Log("msg", "failed to commit offset", "err", lastErr, "attempt", b.NumRetries())
		b.Wait()
	}
	return errors.Join(b.Err(), lastErr)
}

func (c *flushCommitterImpl) observeResult(duration time.Duration, err error) {
	c.duration.WithLabelValues(resultOf(err)).Observe(duration.Seconds())
}

// resultOf returns the result label value for an operation that returned err.
func resultOf(err error) string {
	switch {
	case err == nil:
		return resultOK
	case errors.Is(err, context.Canceled):
		return resultCancelled
	default:
		return resultError
	}
}
