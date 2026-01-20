package consumer

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/coder/quartz"
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
	"github.com/grafana/loki/v3/pkg/scratch"
)

// builder allows mocking of [logsobj.Builder] in tests.
type builder interface {
	Append(tenant string, stream logproto.Stream) error
	GetEstimatedSize() int
	Flush() (*dataobj.Object, io.Closer, error)
	TimeRanges() []multitenancy.TimeRange
	UnregisterMetrics(prometheus.Registerer)
	CopyAndSort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// flusher allows mocking of flushes in tests.
type flusher interface {
	FlushAsync(ctx context.Context, builder builder, startTime time.Time, offset int64, done func(error))
}

type partitionProcessor struct {
	*services.BasicService

	// Kafka client and topic/partition info
	topic     string
	partition int32
	// lastRecord contains the last record appended to the builder. It is used
	// to commit the correct offset after a flush.
	lastRecord *kgo.Record
	builder    builder
	decoder    *kafka.Decoder

	// Builder initialization
	builderOnce  sync.Once
	builderCfg   logsobj.BuilderConfig
	scratchStore scratch.Store

	// Idle stream handling
	idleFlushTimeout time.Duration
	// Handling flushing dataobjs even if they are not full or idle for too long.
	maxBuilderAge time.Duration

	// lastModified is used to know when the idle is exceeded.
	// The initial value is zero and must be reset to zero after each flush.
	lastModified time.Time

	// earliestRecordTime tracks the earliest timestamp all the records Appended to the builder for each object.
	earliestRecordTime time.Time
	// firstAppendTime tracks the time of the first append to the builder, as `earliestRecordTime` is highly influenced by scenarios of severe lagging.
	firstAppendTime time.Time

	// Metrics
	metrics *partitionOffsetMetrics

	// Control and coordination
	reg    prometheus.Registerer
	logger log.Logger

	recordsChan chan *kgo.Record
	flusher     flusher

	// Used for tests.
	clock quartz.Clock
}

func newPartitionProcessor(
	builderCfg logsobj.BuilderConfig,
	scratchStore scratch.Store,
	idleFlushTimeout time.Duration,
	maxBuilderAge time.Duration,
	topic string,
	partition int32,
	recordsChan chan *kgo.Record,
	flusher flusher,
	logger log.Logger,
	reg prometheus.Registerer,
) *partitionProcessor {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		panic(err)
	}

	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     topic,
		"partition": strconv.Itoa(int(partition)),
	}, reg)

	metrics := newPartitionOffsetMetrics()
	if err := metrics.register(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register partition metrics", "err", err)
	}

	p := &partitionProcessor{
		topic:            topic,
		partition:        partition,
		logger:           logger,
		decoder:          decoder,
		reg:              reg,
		builderCfg:       builderCfg,
		scratchStore:     scratchStore,
		metrics:          metrics,
		idleFlushTimeout: idleFlushTimeout,
		maxBuilderAge:    maxBuilderAge,
		clock:            quartz.NewReal(),
		recordsChan:      recordsChan,
		flusher:          flusher,
	}
	p.BasicService = services.NewBasicService(p.starting, p.running, p.stopping)
	return p
}

// starting implements [services.StartingFn].
func (p *partitionProcessor) starting(_ context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (p *partitionProcessor) running(ctx context.Context) error {
	return p.Run(ctx)
}

// running implements [services.StoppingFn].
func (p *partitionProcessor) stopping(_ error) error {
	return nil
}

func (p *partitionProcessor) Run(ctx context.Context) error {
	defer func() {
		level.Info(p.logger).Log("msg", "stopped partition processor")
	}()
	level.Info(p.logger).Log("msg", "started partition processor")
	for {
		select {
		case <-ctx.Done():
			level.Info(p.logger).Log("msg", "stopping partition processor, context canceled")
			return ctx.Err()
		case record, ok := <-p.recordsChan:
			if !ok {
				level.Info(p.logger).Log("msg", "stopping partition processor, channel closed")
				return nil
			}
			p.processRecord(ctx, record)
		// This partition is idle, flush it.
		case <-time.After(p.idleFlushTimeout):
			if _, err := p.idleFlush(ctx); err != nil {
				level.Error(p.logger).Log("msg", "failed to idle flush", "err", err)
			}
		}
	}
}

func (p *partitionProcessor) initBuilder() error {
	var initErr error
	p.builderOnce.Do(func() {
		// Dataobj builder
		builder, err := logsobj.NewBuilder(p.builderCfg, p.scratchStore)
		if err != nil {
			initErr = err
			return
		}
		if err := builder.RegisterMetrics(p.reg); err != nil {
			initErr = err
			return
		}
		p.builder = builder
	})
	return initErr
}

func (p *partitionProcessor) processRecord(ctx context.Context, record *kgo.Record) {
	p.metrics.processedRecords.Inc()

	// Update offset metric at the end of processing
	defer p.metrics.updateOffset(record.Offset)

	if record.Timestamp.Before(p.earliestRecordTime) || p.earliestRecordTime.IsZero() {
		p.earliestRecordTime = record.Timestamp
	}

	// Observe processing delay
	p.metrics.observeProcessingDelay(record.Timestamp)

	// Initialize builder if this is the first record
	if err := p.initBuilder(); err != nil {
		level.Error(p.logger).Log("msg", "failed to initialize builder", "err", err)
		return
	}

	tenant := string(record.Key)
	stream, err := p.decoder.DecodeWithoutLabels(record.Value)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to decode record", "err", err)
		return
	}

	p.metrics.processedBytes.Add(float64(stream.Size()))

	if p.shouldFlushDueToMaxAge() {
		p.metrics.incFlushesTotal(FlushReasonMaxAge)
		if err := p.flush(ctx); err != nil {
			p.metrics.flushFailures.Inc()
			level.Error(p.logger).Log("msg", "failed to flush and commit dataobj that reached max age", "err", err)
			return
		}
	}

	if err := p.builder.Append(tenant, stream); err != nil {
		if !errors.Is(err, logsobj.ErrBuilderFull) {
			level.Error(p.logger).Log("msg", "failed to append stream", "err", err)
			p.metrics.incAppendFailures()
			return
		}

		p.metrics.incFlushesTotal(FlushReasonBuilderFull)
		if err := p.flush(ctx); err != nil {
			p.metrics.flushFailures.Inc()
			level.Error(p.logger).Log("msg", "failed to flush and commit", "err", err)
			return
		}

		if err := p.builder.Append(tenant, stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
			p.metrics.incAppendFailures()
		}
	}

	if p.firstAppendTime.IsZero() {
		p.firstAppendTime = p.clock.Now()
	}

	p.lastRecord = record
	p.lastModified = p.clock.Now()
}

func (p *partitionProcessor) shouldFlushDueToMaxAge() bool {
	return p.maxBuilderAge > 0 &&
		p.builder.GetEstimatedSize() > 0 &&
		!p.firstAppendTime.IsZero() &&
		p.clock.Since(p.firstAppendTime) > p.maxBuilderAge
}

// idleFlush flushes the partition if it has exceeded the idle flush timeout.
// It returns true if the partition was flushed, false with a non-nil error
// if the partition could not be flushed, and false with a nil error if
// the partition has not exceeded the timeout.
func (p *partitionProcessor) idleFlush(ctx context.Context) (bool, error) {
	if !p.needsIdleFlush() {
		return false, nil
	}
	p.metrics.incFlushesTotal(FlushReasonIdle)
	if err := p.flush(ctx); err != nil {
		p.metrics.flushFailures.Inc()
		return false, err
	}
	return true, nil
}

// needsIdleFlush returns true if the partition has exceeded the idle timeout
// and the builder has some data buffered.
func (p *partitionProcessor) needsIdleFlush() bool {
	// This is a safety check to make sure we never flush empty data objects.
	// It should never happen that lastModified is non-zero while the builder
	// is either uninitialized or empty.
	if p.builder == nil || p.builder.GetEstimatedSize() == 0 {
		return false
	}
	if p.lastModified.IsZero() {
		return false
	}
	return p.clock.Since(p.lastModified) > p.idleFlushTimeout
}

func (p *partitionProcessor) flush(ctx context.Context) error {
	var (
		err  error
		done = make(chan struct{})
	)
	defer func() {
		// Reset the state to prepare for building the next data object.
		p.earliestRecordTime = time.Time{}
		p.firstAppendTime = time.Time{}
		p.lastModified = time.Time{}
		p.lastRecord = nil
	}()
	p.flusher.FlushAsync(ctx, p.builder, p.earliestRecordTime, p.lastRecord.Offset, func(flushErr error) {
		err = flushErr
		close(done)
	})
	<-done
	return err
}
