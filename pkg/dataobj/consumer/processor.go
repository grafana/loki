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
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// A builder allows mocking of [logsobj.Builder] in tests.
type builder interface {
	Append(tenant string, stream logproto.Stream, recTime time.Time) error
	GetEstimatedSize() int
	Flush() (*dataobj.Object, io.Closer, error)
	TimeRanges() []multitenancy.TimeRange
	EarliestRecordTime() time.Time
	CopyAndSort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
	IsFull() bool
}

type builderGroup interface {
	Append(tenant string, stream logproto.Stream, recTime time.Time) error
	GetEstimatedSize() int
	Reset()
	GetBuilders() []builder
	IsFull() bool
}

// A flushCommitter allows mocking of flushes in tests.
type flushCommitter interface {
	Flush(ctx context.Context, builders []builder, reason string, offset int64) error
}

// A processor receives records and builds data objects from them.
// flushRequest is used to send a flush request to the processor's Run loop.
type flushRequest struct {
	done chan<- error
}

type processor struct {
	*services.BasicService
	builder        builderGroup
	decoder        *kafka.Decoder
	records        chan *kgo.Record
	flushCommitter flushCommitter

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

	// flushRequests is used to safely trigger a flush from outside the Run loop.
	mode          IngestMode
	flushRequests chan flushRequest

	metrics *metrics
	logger  log.Logger
}

func newProcessor(
	builder builderGroup,
	records chan *kgo.Record,
	flushCommitter flushCommitter,
	idleFlushTimeout time.Duration,
	maxBuilderAge time.Duration,
	mode IngestMode,
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
		flushCommitter:   flushCommitter,
		flushRequests:    make(chan flushRequest, 1),
		idleFlushTimeout: idleFlushTimeout,
		maxBuilderAge:    maxBuilderAge,
		metrics:          newMetrics(reg),
		logger:           logger,
		mode:             mode,
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

// stopping implements [services.StoppingFn]. It drains any buffered records
// from the in-process channel (up to a 30s timeout) before returning, then
// flushes any accumulated data. This ensures that records buffered at SIGTERM
// are not silently lost in inmemory mode.
//
// Note: stopping() is called after Run() returns (dskit guarantees
// RunningFn happens-before StoppingFn), so there is no race with the run loop.
// The records channel remains open (owned by Service) and may still have
// buffered records written by the distributor before the push timeout fired.
func (p *processor) stopping(_ error) error {
	if p.mode == IngestModeInMemory {
		p.drainOnShutdown()
	}
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
		case req := <-p.flushRequests:
			// Drain any records that are already in the channel before flushing
			// so that all pending data is included in the flush.
		drain:
			for {
				select {
				case rec, ok := <-p.records:
					if !ok {
						break drain
					}
					if err := p.processRecord(ctx, rec); err != nil {
						level.Error(p.logger).Log("msg", "failed to process record during flush drain", "err", err)
						p.observeRecordErr(rec)
					}
				default:
					break drain
				}
			}
			var err error
			if !p.lastAppend.IsZero() && p.builder.GetEstimatedSize() > 0 {
				err = p.flush(ctx, flushReasonIdle)
			}
			req.done <- err
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

	if p.builder.IsFull() {
		if err := p.flush(ctx, flushReasonBuilderFull); err != nil {
			return fmt.Errorf("failed to flush: %w", err)
		}
	}

	if err := p.builder.Append(tenant, stream, rec.Timestamp); err != nil {
		return fmt.Errorf("failed to append stream: %w", err)
	}
	p.metrics.sizeEstimate.Set(float64(p.builder.GetEstimatedSize()))

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

// Flush triggers an immediate flush via the processor's Run loop, draining any
// pending records from the channel first. Safe to call from any goroutine.
func (p *processor) Flush(ctx context.Context) error {
	done := make(chan error, 1)
	select {
	case p.flushRequests <- flushRequest{done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		// Reset the state to prepare for building the next data object. The
		// reset runs on every outcome: success (obvious), ctx canceled (we're
		// shutting down — state is no longer needed), and panic (the process
		// is about to crash and Kafka will replay from the last committed
		// offset).
		p.firstAppend = time.Time{}
		p.lastAppend = time.Time{}
		p.builder.Reset()
		p.metrics.sizeEstimate.Set(0)
	}()

	timeWindowedBuilders := p.builder.GetBuilders()
	p.metrics.timePartitionEstimate.Add(float64(len(timeWindowedBuilders)))
	err := p.flushCommitter.Flush(ctx, timeWindowedBuilders, reason, p.lastOffset)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if ctx.Err() != nil {
		level.Info(p.logger).Log("msg", "flush returned a non-cancellation error while the context was canceled", "reason", reason, "err", err)
		return ctx.Err()
	}
	// logsobj.Builder.Flush is not re-entrant: once it consumes the buffered
	// state, a retry cannot reproduce the object. Any flushCommitter error
	// that isn't a graceful shutdown therefore escalates to a panic so the
	// process restarts and Kafka replays from the last committed offset.
	panic(err)
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

func (p *processor) drainOnShutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dropped := 0
drain:
	for {
		select {
		case rec, ok := <-p.records:
			if !ok {
				break drain
			}
			if err := p.processRecord(ctx, rec); err != nil {
				level.Error(p.logger).Log("msg", "failed to process record during shutdown drain", "err", err)
				p.observeRecordErr(rec)
			}
		case <-ctx.Done():
			// Drain timed out — count remaining buffered records as dropped.
			dropped = len(p.records)
			break drain
		default:
			// Channel is empty — drain complete.
			break drain
		}
	}

	if dropped > 0 {
		level.Warn(p.logger).Log("msg", "inmemory drain timed out, records dropped", "count", dropped)
	} else {
		level.Info(p.logger).Log("msg", "inmemory channel drained cleanly on shutdown")
	}

	// Flush whatever was accumulated during drain.
	if !p.lastAppend.IsZero() && p.builder.GetEstimatedSize() > 0 {
		if err := p.flush(ctx, "shutdown"); err != nil {
			level.Error(p.logger).Log("msg", "failed to flush during shutdown drain", "err", err)
		}
	}
}
