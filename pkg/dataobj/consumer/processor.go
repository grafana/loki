package consumer

import (
	"context"
	"errors"
	"io"
	"strconv"
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

// A flusher allows mocking of flushes in tests.
type flusher interface {
	FlushAsync(ctx context.Context, builder builder, startTime time.Time, offset int64, done func(error))
}

// A processor receives records and builds data objects from them.
type processor struct {
	*services.BasicService
	builder     builder
	decoder     *kafka.Decoder
	recordsChan chan *kgo.Record
	flusher     flusher

	// offset contains the offset of the last record appended to the data object
	// builder. It is used to commit the correct offset after a flush.
	offset int64

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

	// Metrics
	metrics *partitionOffsetMetrics
	logger  log.Logger
}

func newProcessor(
	builder builder,
	idleFlushTimeout time.Duration,
	maxBuilderAge time.Duration,
	topic string,
	partition int32,
	recordsChan chan *kgo.Record,
	flusher flusher,
	logger log.Logger,
	reg prometheus.Registerer,
) *processor {
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

	p := &processor{
		builder:          builder,
		logger:           logger,
		decoder:          decoder,
		metrics:          metrics,
		idleFlushTimeout: idleFlushTimeout,
		maxBuilderAge:    maxBuilderAge,
		recordsChan:      recordsChan,
		flusher:          flusher,
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
	defer func() {
		level.Info(p.logger).Log("msg", "stopped partition processor")
	}()
	level.Info(p.logger).Log("msg", "started partition processor")
	for {
		select {
		case <-ctx.Done():
			level.Info(p.logger).Log("msg", "stopping partition processor, context canceled")
			// We don't return ctx.Err() here as it manifests as a service failure
			// when shutting down.
			return nil
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

func (p *processor) processRecord(ctx context.Context, record *kgo.Record) {
	now := time.Now()
	p.metrics.processedRecords.Inc()

	// Update offset metric at the end of processing
	defer p.metrics.updateOffset(record.Offset)

	if record.Timestamp.Before(p.earliestRecordTime) || p.earliestRecordTime.IsZero() {
		p.earliestRecordTime = record.Timestamp
	}

	// Observe processing delay
	p.metrics.observeProcessingDelay(record.Timestamp)

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

	if p.firstAppend.IsZero() {
		p.firstAppend = now
	}
	p.lastAppend = now
	p.offset = record.Offset
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
	p.metrics.incFlushesTotal(FlushReasonIdle)
	if err := p.flush(ctx); err != nil {
		p.metrics.flushFailures.Inc()
		return false, err
	}
	return true, nil
}

// needsIdleFlush returns true if the partition has exceeded the idle timeout
// and the builder has some data buffered.
func (p *processor) needsIdleFlush() bool {
	// This is a safety check to make sure we never flush empty data objects.
	// It should never happen that lastModified is non-zero while the builder
	// is either uninitialized or empty.
	if p.builder == nil || p.builder.GetEstimatedSize() == 0 {
		return false
	}
	if p.lastAppend.IsZero() {
		return false
	}
	return time.Since(p.lastAppend) > p.idleFlushTimeout
}

func (p *processor) flush(ctx context.Context) error {
	var (
		err  error
		done = make(chan struct{})
	)
	defer func() {
		// Reset the state to prepare for building the next data object.
		p.earliestRecordTime = time.Time{}
		p.firstAppend = time.Time{}
		p.lastAppend = time.Time{}
	}()
	p.flusher.FlushAsync(ctx, p.builder, p.earliestRecordTime, p.offset, func(flushErr error) {
		err = flushErr
		close(done)
	})
	<-done
	return err
}
