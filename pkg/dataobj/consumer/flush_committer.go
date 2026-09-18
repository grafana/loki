package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// A committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, partition int32, offset int64) error
}

// An indexer allows mocking of index building in tests.
type indexer interface {
	Index(ctx context.Context, obj *dataobj.Object, objPath string) error
}

// A flusher allows mocking of flushes in tests.
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
	partition int32
	logger    log.Logger

	// Metrics.
	commits                prometheus.Counter
	commitFailures         prometheus.Counter
	endToEndProcessingTime prometheus.Gauge
}

func newFlushCommitter(
	flusher flusher,
	committer committer,
	indexer indexer,
	partition int32,
	logger log.Logger,
	r prometheus.Registerer,
) *flushCommitterImpl {
	return &flushCommitterImpl{
		flusher:   flusher,
		committer: committer,
		indexer:   indexer,
		partition: partition,
		logger:    logger,
		commits: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits.",
		}),
		commitFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures.",
		}),
		endToEndProcessingTime: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingest_end_to_end_processing_time_seconds",
			Help: "Time between a log line being written to Kafka by the distributors and it becoming available for querying in seconds.",
		}),
	}
}

// Flush multiple data object builders and, if successful, commit the offset.
func (c *flushCommitterImpl) Flush(ctx context.Context, builders []builder, reason string, offset int64) error {
	// Read before flushing: flushing resets the builders, which clears their
	// earliest record times.
	earliest := earliestRecordTime(builders)

	for _, b := range builders {
		if err := c.flushOne(ctx, b, reason); err != nil {
			return err
		}
	}

	// Everything above is queryable once its index is in the metastore, so the
	// oldest record in this flush is the one that determines ingest lag.
	if !earliest.IsZero() {
		c.endToEndProcessingTime.Set(time.Since(earliest).Seconds())
	}

	// commit returns an error only if context is cancelled, otherwise it retries indefinitely
	if err := c.commit(ctx, offset); err != nil {
		c.commitFailures.Inc()
		return fmt.Errorf("failed to commit data object offset %d: %w", offset, err)
	}
	return nil
}

func (c *flushCommitterImpl) flushOne(ctx context.Context, builder builder, reason string) error {
	obj, objCloser, objPath, err := c.flusher.Flush(ctx, builder, reason)
	if err != nil {
		return fmt.Errorf("failed to flush data object: %w", err)
	}
	// Releasing the object never fails the flush: it is already uploaded, and
	// the flusher counts and logs any failure.
	defer func() { _ = objCloser.Close() }()

	// index returns an error only if the context is canceled, otherwise it
	// retries indefinitely.
	if err := c.index(ctx, obj, objPath); err != nil {
		return fmt.Errorf("failed to index data object: %w", err)
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

// index builds and uploads the index for the object and records it in the
// metastore, retrying with exponential backoff until successful or the context
// is canceled. Retrying is safe because a failed index is discarded before the
// metastore is touched.
func (c *flushCommitterImpl) index(ctx context.Context, obj *dataobj.Object, objPath string) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	var lastErr error
	for b.Ongoing() {
		lastErr = c.indexer.Index(ctx, obj, objPath)
		if lastErr == nil {
			return nil
		}
		level.Warn(c.logger).Log("msg", "failed to index data object", "err", lastErr, "attempt", b.NumRetries())
		b.Wait()
	}
	// The loop only gives up once the context is done. Surface that alongside
	// the last failure so the processor shuts down gracefully instead of
	// treating it as an unrecoverable flush.
	return errors.Join(b.Err(), lastErr)
}

// commits the offset, retries with exponential backoff until successful or
// the context is canceled.
func (c *flushCommitterImpl) commit(ctx context.Context, offset int64) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	c.commits.Inc()
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
	return lastErr
}
