package builder

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
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type multiBuilder interface {
	Append(tenant string, stream logproto.Stream, recTime time.Time) error
	GetEstimatedSize() int
	IsFull() bool
	// Reset drops the builders. It must not change them, because a flush can
	// still use the builders that getBuilders returned.
	Reset()
	getBuilders() []builder
}

// A builder allows mocking of [logsobj.Builder] in tests.
type builder interface {
	Append(tenant string, stream logproto.Stream, recTime time.Time) error
	GetEstimatedSize() int
	IsFull() bool
	Flush() (*dataobj.Object, io.Closer, error)
	TimeRanges() []dataobj.TimeRange
	GetEarliestRecordTime() time.Time
	CopyAndSort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// A flushCommitter allows mocking of flushes in tests.
type flushCommitter interface {
	Flush(ctx context.Context, builder []builder, reason string, offset int64) error
}

// A processor receives records and builds data objects from them.
type processor struct {
	*services.BasicService
	builder        multiBuilder
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

	// flushes sends flush requests to the flush loop. It has no buffer, so a
	// send blocks while the loop runs the previous flush. The processor then
	// holds the builders of at most two flushes: the one that runs and the one
	// that waits.
	flushes chan flushRequest

	// flushFailed receives the error of the first flush that fails before a
	// shutdown. The flush loop stops after it sends the error.
	flushFailed chan error

	// The flush loop closes flushLoopDone when it stops.
	flushLoopDone chan struct{}

	metrics *metrics
	logger  log.Logger
}

// A flushRequest asks the flush loop to flush builders and commit offset.
type flushRequest struct {
	builders []builder
	reason   string
	// offset is the biggest offset of the records in the builders.
	offset int64
}

func newProcessor(
	builder multiBuilder,
	records chan *kgo.Record,
	flushCommitter flushCommitter,
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
		flushCommitter:   flushCommitter,
		idleFlushTimeout: idleFlushTimeout,
		maxBuilderAge:    maxBuilderAge,
		flushes:          make(chan flushRequest),
		flushFailed:      make(chan error, 1),
		flushLoopDone:    make(chan struct{}),
		metrics:          newMetrics(reg),
		logger:           logger,
	}
	p.BasicService = services.NewBasicService(p.starting, p.running, p.stopping).WithName("dataobj-builder-processor")
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

// errStopProcessing means that the flush loop stopped or ctx is canceled, so
// the processor cannot flush any more. Run stops on it. stopFlushLoop returns
// the error of a failed flush, if any.
var errStopProcessing = errors.New("processor must stop")

func (p *processor) Run(ctx context.Context) (returnErr error) {
	defer level.Info(p.logger).Log("msg", "stopped partition processor")
	level.Info(p.logger).Log("msg", "started partition processor")

	// The flush loop must not outlive the processor. It uses ctx, so it stops
	// soon after a shutdown cancels ctx. Run returns the error of a failed
	// flush, which only stopFlushLoop reads.
	p.startFlushLoop(ctx)
	defer func() {
		if err := p.stopFlushLoop(); err != nil {
			// The error fails the service. The builder restarts and reads the
			// records again from the last commit.
			level.Error(p.logger).Log("msg", "terminating after failed flush", "err", err)
			returnErr = errors.Join(returnErr, err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			level.Info(p.logger).Log("msg", "context canceled")
			// We don't return ctx.Err() here as it manifests as a service failure
			// when stopping the service.
			return nil
		case <-p.flushLoopDone:
			// The processor stops now, not at the next flush, so it does not
			// append records that it can never commit.
			return nil
		case rec, ok := <-p.records:
			if !ok {
				level.Info(p.logger).Log("msg", "channel closed")
				return nil
			}
			if err := p.processRecord(ctx, rec); err != nil {
				// The record is not lost if the processor stops: a restart
				// reads it again from the last commit. The deferred
				// stopFlushLoop returns the error of a failed flush.
				if errors.Is(err, errStopProcessing) {
					return nil
				}
				level.Error(p.logger).Log("msg", "failed to process record", "err", err)
				p.observeRecordErr(rec)
			}
		case <-time.After(p.idleFlushTimeout):
			// This partition is idle, flush it.
			p.metrics.setConsumptionLag(0)
			if _, err := p.idleFlush(ctx); errors.Is(err, errStopProcessing) {
				return nil
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
			return err
		}
	}

	if p.builder.IsFull() {
		if err := p.flush(ctx, flushReasonBuilderFull); err != nil {
			return err
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

// idleFlush sends a flush request to the flush loop if the partition has
// exceeded the idle flush timeout. It returns true if it sent the request,
// false with errStopProcessing if it could not send it, and false with a nil
// error if the partition has not exceeded the timeout.
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

// flush sends the builders to the flush loop as a flush request. The loop
// flushes them and commits the offset of the last appended record, while the
// processor keeps appending records to new builders.
//
// flush waits while the loop runs the previous flush. It returns
// errStopProcessing if the loop stopped or ctx is canceled.
func (p *processor) flush(ctx context.Context, reason string) error {
	req := flushRequest{
		builders: p.builder.getBuilders(),
		reason:   reason,
		offset:   p.lastOffset,
	}

	// Reset the state to prepare for building the next data objects. Reset
	// drops the builders from p.builder, and the flush request keeps them.
	p.firstAppend = time.Time{}
	p.lastAppend = time.Time{}
	p.builder.Reset()

	// The size estimate includes these builders until the flush loop takes
	// them.
	defer p.metrics.sizeEstimate.Set(0)

	waitStart := time.Now()
	select {
	case p.flushes <- req:
		p.metrics.flushWaitSeconds.Add(time.Since(waitStart).Seconds())
		return nil
	case <-p.flushLoopDone:
		return errStopProcessing
	case <-ctx.Done():
		return errStopProcessing
	}
}

// startFlushLoop starts the goroutine that runs the flushes from p.flushes,
// one at a time and in order. The goroutine stops at the first failure, or
// after a flush once ctx is canceled. After it stops it runs no more flushes,
// so no commit passes a failed or canceled flush. A restart reads those
// records again from the last commit.
func (p *processor) startFlushLoop(ctx context.Context) {
	go func() {
		defer close(p.flushLoopDone)
		for req := range p.flushes {
			err := p.flushCommitter.Flush(ctx, req.builders, req.reason, req.offset)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				// The builders are already reset, so the processor cannot flush
				// them again. A restart reads the records again from Kafka.
				p.flushFailed <- fmt.Errorf("%s flush up to offset %d: %w", req.reason, req.offset, err)
				return
			}
		}
	}()
}

// stopFlushLoop waits for the flush in progress and stops the flush loop. It
// returns the error of a failed flush, or nil. The processor must not call
// flush after stopFlushLoop.
func (p *processor) stopFlushLoop() error {
	close(p.flushes)
	<-p.flushLoopDone
	select {
	case err := <-p.flushFailed:
		return err
	default:
		return nil
	}
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
