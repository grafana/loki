package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// A committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, partition int32, offset int64) error
}

// A metastoreEventEmitter allows mocking of [metastoreEvents] in tests.
type metastoreEventEmitter interface {
	Emit(ctx context.Context, objectPath string, earliestRecordTime time.Time) error
}

// A flusher allows mocking of flushes in tests.
type flusher interface {
	Flush(ctx context.Context, builder builder, reason string) (string, error)
}

// A flushManagerImpl manages the flushing of data objects and commits.
type flushManagerImpl struct {
	flusher         flusher
	metastoreEvents metastoreEventEmitter
	committer       committer
	partition       int32
	logger          log.Logger

	// Metrics.
	commits        prometheus.Counter
	commitFailures prometheus.Counter
}

func newFlushManager(
	flusher flusher,
	metastoreEvents metastoreEventEmitter,
	committer committer,
	partition int32,
	logger log.Logger,
	r prometheus.Registerer,
) *flushManagerImpl {
	return &flushManagerImpl{
		flusher:         flusher,
		metastoreEvents: metastoreEvents,
		committer:       committer,
		partition:       partition,
		logger:          logger,
		commits: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits.",
		}),
		commitFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures.",
		}),
	}
}

// Flush the data object builder and, if successful, commit the offset.
func (m *flushManagerImpl) Flush(ctx context.Context, builder builder, reason string, offset int64, earliestRecordTime time.Time) error {
	objectPath, err := m.flusher.Flush(ctx, builder, reason)
	if err != nil {
		return fmt.Errorf("failed to flush data object: %w", err)
	}
	if err := m.emitEvent(ctx, objectPath, earliestRecordTime); err != nil {
		return fmt.Errorf("failed to emit metastore event: %w", err)
	}
	if err := m.commit(ctx, offset); err != nil {
		m.commitFailures.Inc()
		return fmt.Errorf("failed to commit data object: %w", err)
	}
	return nil
}

// emitEvent emits a metastore event for the object, retries with exponential
// backoff until successful or the context is canceled.
func (m *flushManagerImpl) emitEvent(ctx context.Context, objectPath string, earliestRecordTime time.Time) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	var lastErr error
	for b.Ongoing() {
		lastErr = m.metastoreEvents.Emit(ctx, objectPath, earliestRecordTime)
		if lastErr == nil {
			break
		}
		level.Warn(m.logger).Log("msg", "failed to emit metastore event", "err", lastErr, "attempt", b.NumRetries())
		b.Wait()
	}
	return lastErr
}

// commits the offset, retries with exponential backoff until successful or
// the context is canceled.
func (m *flushManagerImpl) commit(ctx context.Context, offset int64) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	m.commits.Inc()
	var lastErr error
	for b.Ongoing() {
		lastErr = m.committer.Commit(ctx, m.partition, offset)
		if lastErr == nil {
			level.Debug(m.logger).Log("msg", "committed offset", "partition", m.partition, "offset", offset)
			break
		}
		level.Warn(m.logger).Log("msg", "failed to commit offset", "err", lastErr, "attempt", b.NumRetries())
		b.Wait()
	}
	return lastErr
}
