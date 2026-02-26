package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// A builder allows mocking of [logsobj.Builder] in tests.
type builder interface {
	Append(tenant string, stream logproto.Stream) error
	GetEstimatedSize() int
	Flush() (*dataobj.Object, io.Closer, error)
	TimeRanges() []multitenancy.TimeRange
	CopyAndSort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// A flushManager allows mocking of flushes in tests.
type flushManager interface {
	Flush(ctx context.Context, builder builder, reason string, offset int64, earliestRecordTime time.Time) error
}

// A processor receives records and builds data objects from them.
type processor struct {
	*services.BasicService
	builder      builder
	decoder      *kafka.Decoder
	records      chan *kgo.Record
	flushManager flushManager

	// lastOffset contains the offset of the last record appended to the data object
	// builder. It is used to commit the correct offset after a flush.
	lastOffset int64

	// idleFlushTimeout is the maximum amount of time to wait for more data. If no
	// records are received within this timeout, the current data object builder
	// is flushed. Most of the time, this timeout occurs when a partition is
	// marked as inactive in preparation for a scale down.
	idleFlushTimeout time.Duration

	// maxBuilderAge is the maximum age a data object builder can reach before it
	// must be flushed. This happens if a partition does not receive enough data
	// to reach the target size within the allocated time. We would rather flush
	// a small data object and compact it later then wait for more data to arrive
	// and delay querying.
	maxBuilderAge time.Duration

	// firstAppend tracks the wall clock time of the first append to the data
	// object builder. It is used to know if the builder has exceeded the
	// maximum age. It must be reset after each flush.
	firstAppend time.Time

	// lastAppend tracks the wall clock time of the last append to the data
	// object builder. It is used to know if the builder has exceeded the
	// idle timeout. It must be reset after each flush.
	lastAppend time.Time

	// earliestRecordTime tracks the timestamp of the earliest record appended
	// to the data object builder. It is required for the metastore index.
	earliestRecordTime time.Time

	metrics *metrics
	logger  log.Logger
}

func newProcessor(
	builder builder,
	records chan *kgo.Record,
	flushManager flushManager,
	idleFlushTimeout time.Duration,
	maxBuilderAge time.Duration,
	logger log.Logger,
	reg prometheus.Registerer,
) *processor {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		panic(err)
	}
	p := &processor{
		builder:          builder,
		decoder:          decoder,
		records:          records,
		flushManager:     flushManager,
		idleFlushTimeout: idleFlushTimeout,
		maxBuilderAge:    maxBuilderAge,
		metrics:          newMetrics(reg),
		logger:           logger,
	}
	p.BasicService = services.NewBasicService(p.starting, p.running, p.stopping)
	return p
}

// starting implements [services.StartingFn].
func (p *processor) starting(_ context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (p *processor) running(ctx context.Context) error {
	return p.Run(ctx)
}

// stopping implements [services.StoppingFn].
func (p *processor) stopping(_ error) error {
	return nil
}

func (p *processor) Run(ctx context.Context) error {
	defer level.Info(p.logger).Log("msg", "stopped partition processor")
	level.Info(p.logger).Log("msg", "started partition processor")
	for {
		select {
		case <-ctx.Done():
			level.Info(p.logger).Log("msg", "context canceled")
			// We don't return ctx.Err() here as it manifests as a service failure
			// when stopping the service.
			return nil
		case rec, ok := <-p.records:
			if !ok {
				level.Info(p.logger).Log("msg", "channel closed")
				return nil
			}
			if err := p.processRecord(ctx, rec); err != nil {
				level.Error(p.logger).Log("msg", "failed to process record", "err", err)
				p.observeRecordErr(rec)
			}
		case <-time.After(p.idleFlushTimeout):
			// This partition is idle, flush it.
			p.metrics.setConsumptionLag(0)
			if _, err := p.idleFlush(ctx); err != nil {
				level.Error(p.logger).Log("msg", "failed to idle flush", "err", err)
			}
		}
	}
}

func (p *processor) processRecord(ctx context.Context, rec *kgo.Record) error {
	// We use the current time to calculate a number of metrics, such as
	// consumption lag, the age of the data object builder, etc.
	now := time.Now()
	p.observeRecord(rec, now)

	// Try to decode the stream in the record.
	tenant := string(rec.Key)
	stream, err := p.decoder.DecodeWithoutLabels(rec.Value)
	if err != nil {
		// This is an unrecoverable error and no amount of retries will fix it.
		return fmt.Errorf("failed to decode stream: %w", err)
	}

	if p.shouldFlushDueToMaxAge() {
		if err := p.flush(ctx, flushReasonMaxAge); err != nil {
			return fmt.Errorf("failed to flush: %w", err)
		}
	}

	if err := p.builder.Append(tenant, stream); err != nil {
		if !errors.Is(err, logsobj.ErrBuilderFull) {
			return fmt.Errorf("failed to append stream: %w", err)
		}
		if err := p.flush(ctx, flushReasonBuilderFull); err != nil {
			return fmt.Errorf("failed to flush and commit: %w", err)
		}
		if err := p.builder.Append(tenant, stream); err != nil {
			return fmt.Errorf("failed to append stream after flushing: %w", err)
		}
	}

	if p.earliestRecordTime.IsZero() || rec.Timestamp.Before(p.earliestRecordTime) {
		p.earliestRecordTime = rec.Timestamp
	}
	if p.firstAppend.IsZero() {
		p.firstAppend = now
	}
	p.lastAppend = now
	p.lastOffset = rec.Offset

	return nil
}

func (p *processor) shouldFlushDueToMaxAge() bool {
	return p.maxBuilderAge > 0 &&
		p.builder.GetEstimatedSize() > 0 &&
		!p.firstAppend.IsZero() &&
		time.Since(p.firstAppend) > p.maxBuilderAge
}

// idleFlush flushes the partition if it has exceeded the idle flush timeout.
// It returns true if the partition was flushed, false with a non-nil error
// if the partition could not be flushed, and false with a nil error if
// the partition has not exceeded the timeout.
func (p *processor) idleFlush(ctx context.Context) (bool, error) {
	if !p.needsIdleFlush() {
		return false, nil
	}
	if err := p.flush(ctx, flushReasonIdle); err != nil {
		return false, err
	}
	return true, nil
}

// needsIdleFlush returns true if the partition has exceeded the idle timeout
// and the builder has some data buffered.
func (p *processor) needsIdleFlush() bool {
	if p.lastAppend.IsZero() {
		return false
	}
	// This is a safety check to make sure we never flush empty data objects.
	// It should never happen that lastAppend is non-zero while the builder
	// is empty.
	if p.builder.GetEstimatedSize() == 0 {
		return false
	}
	return time.Since(p.lastAppend) > p.idleFlushTimeout
}

func (p *processor) flush(ctx context.Context, reason string) error {
	defer func() {
		// Reset the state to prepare for building the next data object.
		p.earliestRecordTime = time.Time{}
		p.firstAppend = time.Time{}
		p.lastAppend = time.Time{}
	}()
	return p.flushManager.Flush(ctx, p.builder, reason, p.lastOffset, p.earliestRecordTime)
}

func (p *processor) observeRecord(rec *kgo.Record, now time.Time) {
	p.metrics.records.Inc()
	p.metrics.receivedBytes.Add(float64(len(rec.Value)))
	p.metrics.setLastOffset(rec.Offset)
	p.metrics.setConsumptionLag(now.Sub(rec.Timestamp))
}

func (p *processor) observeRecordErr(rec *kgo.Record) {
	p.metrics.recordFailures.Inc()
	p.metrics.discardedBytes.Add(float64(len(rec.Value)))
}
