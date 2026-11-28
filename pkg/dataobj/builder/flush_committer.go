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
	// WriteEntriesWithWatermark adds all entries and raises the watermark in
	// one write.
	WriteEntriesWithWatermark(ctx context.Context, tenant string, entries []metastore.TableOfContentsEntry, update metastore.WatermarkUpdate) error
}

// A flusher allows mocking of flushes in tests. Implementations must be safe
// for concurrent use.
type flusher interface {
	// Flush builds, sorts and uploads the builder's data object. On success the
	// caller owns the returned [io.Closer] and must close it once it is done
	// reading the object; reads of the object fail after that. If an error is
	// returned the closer is always nil.
	Flush(ctx context.Context, builder logsBuilder, reason string) (*dataobj.Object, io.Closer, string, error)
}

// A flushCommitterImpl manages the flushing of data objects and commits.
type flushCommitterImpl struct {
	flusher   flusher
	committer committer
	indexer   indexer
	tocWriter tocWriter
	topic     string
	partition int32
	logger    log.Logger

	// concurrency is the maximum number of ToC windows that one flush has in
	// progress. A ToC window in progress holds the sorted object of one builder at a
	// time, so concurrency also bounds the sorted objects in memory. It is at
	// least 1.
	concurrency int

	metrics *flushCommitterMetrics
}

type flushCommitterMetrics struct {
	duration      *prometheus.HistogramVec
	e2eLagSeconds prometheus.Gauge
}

func newFlushCommitterMetrics(r prometheus.Registerer) *flushCommitterMetrics {
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

	return &flushCommitterMetrics{
		duration: d,
		e2eLagSeconds: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_e2e_lag_seconds",
			Help: "Time between a log line being written to Kafka by the distributors and it becoming available for querying in seconds.",
		}),
	}
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
	topic string,
	partition int32,
	concurrency int,
	logger log.Logger,
	r prometheus.Registerer,
) *flushCommitterImpl {
	return &flushCommitterImpl{
		flusher:     flusher,
		committer:   committer,
		indexer:     indexer,
		tocWriter:   tocWriter,
		topic:       topic,
		partition:   partition,
		logger:      logger,
		concurrency: concurrency,
		metrics:     newFlushCommitterMetrics(r),
	}
}

// Flush flushes the data object builders, adds their indexes to the ToCs with
// the ToC watermark raised to offset, and then commits offset.
func (c *flushCommitterImpl) Flush(ctx context.Context, scopedBuilders []*tocScopedBuilders, reason string, offset int64) (returnErr error) {
	start := time.Now()
	defer func() { c.observeResult(time.Since(start), returnErr) }()

	// Read before flushing: flushing resets the builders, which clears their
	// earliest record times.
	earliest := earliestRecordTime(scopedBuilders)

	if err := c.flushAll(ctx, scopedBuilders, reason, offset); err != nil {
		return err
	}

	// Everything above is queryable once its index is in the metastore, so the
	// oldest record in this flush is the one that determines ingest lag.
	if !earliest.IsZero() {
		c.metrics.e2eLagSeconds.Set(time.Since(earliest).Seconds())
	}

	// commit returns an error only if context is cancelled, otherwise it retries indefinitely
	if err := c.commit(ctx, offset); err != nil {
		return fmt.Errorf("failed to commit data object offset %d: %w", offset, err)
	}
	return nil
}

// flushAll flushes and indexes the builders of each ToC and records the indexes
// in the ToCs, with at most c.concurrency ToCs in progress. It returns once
// every started ToC is done and has released its objects.
//
// The builders of one ToC share one ToC write for their indexes and the
// watermark. With one write for each builder, a crash after the first write
// would leave a watermark that covers the lines of the other builder. The
// restart would then drop those lines.
//
// The first failure cancels the ToCs in progress and skips the ToCs not yet
// started. flushAll returns that first failure, not the cancellation errors
// that it causes in the other ToCs.
func (c *flushCommitterImpl) flushAll(ctx context.Context, scopedBuilders []*tocScopedBuilders, reason string, offset int64) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(c.concurrency)

	ordered := scopedBuilders
	if c.concurrency > 1 {
		// One ToC can have a large builder that takes most of the flush time. If it starts last, it runs alone after the small ones. If it
		// starts first, the small ones run beside it, and the flush ends sooner.
		//
		// With one slot, the order does not change the flush time. Largest
		// first would only delay the ToC entries of the small ToCs.
		//
		// Largest first also builds the largest sorted objects at the same
		// time, which raises peak memory.
		ordered = slices.Clone(scopedBuilders)
		slices.SortStableFunc(ordered, func(a, b *tocScopedBuilders) int {
			return cmp.Compare(b.estimatedSize(), a.estimatedSize())
		})
	}
	for _, sb := range ordered {
		g.Go(func() error {
			// Skip the ToC if the flush was canceled or another ToC
			// failed while this one waited for a slot. FlushAndSort ignores ctx
			// while it builds. It would build the whole object before the sort
			// sees the cancellation.
			if err := gctx.Err(); err != nil {
				return err
			}
			return c.flushTOC(gctx, sb, reason, offset)
		})
	}
	return g.Wait()
}

// flushTOC flushes and indexes the builders of one ToC, one at a time. Then it
// adds all their indexes to the ToC with the watermark raised to offset, in
// one write.
func (c *flushCommitterImpl) flushTOC(ctx context.Context, sb *tocScopedBuilders, reason string, offset int64) error {
	var entries []metastore.TableOfContentsEntry
	for _, b := range sb.all() {
		entry, err := c.flushLogsAndIndex(ctx, b, reason)
		if err != nil {
			return fmt.Errorf("ToC %s: %w", sb.toc, err)
		}
		entries = append(entries, entry)
	}

	// A ToC write error fails the flush, so the offset is not committed and
	// the records are read again.
	if err := c.writeToC(ctx, sb.toc, sb.initialWatermark, entries, offset); err != nil {
		return fmt.Errorf("failed to update metastore ToC %s: %w", sb.toc, err)
	}
	return nil
}

// flushLogsAndIndex uploads and indexes the object of one builder. It returns
// the ToC entry of the index. It releases the object before it returns. The
// entry holds all the data that the ToC write needs.
func (c *flushCommitterImpl) flushLogsAndIndex(ctx context.Context, b logsBuilder, reason string) (metastore.TableOfContentsEntry, error) {
	obj, objCloser, objPath, err := c.flusher.Flush(ctx, b, reason)
	if err != nil {
		return metastore.TableOfContentsEntry{}, fmt.Errorf("failed to flush data object: %w", err)
	}
	// Releasing the object never fails the flush: it is already uploaded, and
	// the flusher counts and logs any failure.
	defer func() { _ = objCloser.Close() }()

	// index returns an error if the context is canceled or the object can't
	// be processed. It retries every other error indefinitely.
	res, err := c.index(ctx, obj, objPath)
	if err != nil {
		return metastore.TableOfContentsEntry{}, fmt.Errorf("failed to index data object: %w", err)
	}
	return metastore.TableOfContentsEntry{
		Path:         res.Path,
		StartTime:    res.TimeRange.MinTime,
		EndTime:      res.TimeRange.MaxTime,
		ShardBuckets: res.ShardBuckets,
	}, nil
}

// writeToC adds the indexes to the ToC that toc identifies. In the same write,
// it raises the watermark of the partition to offset. The write applies only if
// the ToC holds the initial watermark of the builders.
func (c *flushCommitterImpl) writeToC(ctx context.Context, toc tocKey, initialWatermark *int64, entries []metastore.TableOfContentsEntry, offset int64) error {
	return c.tocWriter.WriteEntriesWithWatermark(ctx, toc.tenant, entries, metastore.WatermarkUpdate{
		Topic:     c.topic,
		Partition: c.partition,
		EndOffset: offset,
		Expected:  initialWatermark,
	})
}

// earliestRecordTime returns the oldest record time of all builders, or the
// zero time if no builder holds a record. It must be called before the
// builders are flushed, as flushing resets them.
func earliestRecordTime(scopedBuilders []*tocScopedBuilders) time.Time {
	var earliest time.Time
	for _, sb := range scopedBuilders {
		for _, b := range sb.all() {
			t := b.GetEarliestRecordTime()
			if t.IsZero() {
				continue
			}
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
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
	c.metrics.duration.WithLabelValues(resultOf(err)).Observe(duration.Seconds())
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
