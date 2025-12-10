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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
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
	CopyAndSort(obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

type builderWithReg struct {
	builder
	prometheus.Registerer
}

type partitionProcessor struct {
	cfg       logsobj.BuilderConfig
	topic     string
	partition int32

	decoder      *kafka.Decoder
	scratchStore scratch.Store

	flusher        *flusher
	freeBuilders   chan builder
	builderOnce    sync.Once
	builders       []builderWithReg
	currentBuilder builder
	currentOffset  int64

	// lastModified is used to know when the idle flush timeout is exceeded.
	// The initial value is zero, and must be reset to zero each time the
	// current builder is rotated.
	lastModified     time.Time
	idleFlushTimeout time.Duration

	//oldestRecord tracks the timestamp of the oldest record. The initial value
	// is zero and must be reset to zero each time the current builder is
	// rotated.
	oldestRecord time.Time

	// Control and coordination
	wg       sync.WaitGroup
	reg      prometheus.Registerer
	logger   log.Logger
	metrics  *partitionOffsetMetrics
	uploader *uploader.Uploader

	// Used for tests.
	clock quartz.Clock
}

func newPartitionProcessor(
	committer committer,
	builderCfg logsobj.BuilderConfig,
	uploaderCfg uploader.Config,
	metastoreCfg metastore.Config,
	bucket objstore.Bucket,
	scratchStore scratch.Store,
	logger log.Logger,
	reg prometheus.Registerer,
	idleFlushTimeout time.Duration,
	eventsProducerClient *kgo.Client,
	topic string,
	partition int32,
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

	uploader := uploader.New(uploaderCfg, bucket, logger)
	if err := uploader.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register uploader metrics", "err", err)
	}

	return &partitionProcessor{
		topic:            topic,
		partition:        partition,
		logger:           logger,
		decoder:          decoder,
		reg:              reg,
		cfg:              builderCfg,
		scratchStore:     scratchStore,
		metrics:          metrics,
		idleFlushTimeout: idleFlushTimeout,
		clock:            quartz.NewReal(),
		flusher:          newFlusher(uploader, committer, eventsProducerClient, int32(metastoreCfg.PartitionRatio), logger, reg),
		freeBuilders:     make(chan builder, 2),
		currentBuilder:   nil,
		currentOffset:    0,
		uploader:         uploader,
	}
}

func (p *partitionProcessor) Start(ctx context.Context, recordsChan <-chan []partition.Record) func() {
	// Start the flusher.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.flusher.Start(ctx)
		p.flusher.UnregisterMetrics(p.reg)
	}()
	// This is a hack to avoid duplicate metrics registration panics. The
	// problem occurs because [kafka.ReaderService] creates a consumer to
	// process lag on startup, tears it down, and then creates another one
	// once the lag threshold has been met. The new [kafkav2] package will
	// solve this by de-coupling the consumer from processing consumer lag.
	p.wg.Add(1)
	go func() {
		defer func() {
			p.unregisterMetrics()
			level.Info(p.logger).Log("msg", "stopped partition processor")
			p.wg.Done()
		}()
		level.Info(p.logger).Log("msg", "started partition processor")
		for {
			select {
			case <-ctx.Done():
				level.Info(p.logger).Log("msg", "stopping partition processor, context canceled")
				return
			case records, ok := <-recordsChan:
				if !ok {
					level.Info(p.logger).Log("msg", "stopping partition processor, channel closed")
					// Channel was closed. This means no more records will be
					// received. We need to flush what we have to avoid data
					// loss because of how the consumer is torn down between
					// starting and running phases in [kafka.ReaderService].
					if err := p.finalFlush(ctx); err != nil {
						level.Error(p.logger).Log("msg", "failed to flush", "err", err)
					}
					return
				}
				// Process the records received.
				for _, record := range records {
					p.processRecord(ctx, record)
				}
			// This partition is idle, flush it.
			case <-time.After(p.idleFlushTimeout):
				if _, err := p.idleFlush(ctx); err != nil {
					level.Error(p.logger).Log("msg", "failed to idle flush", "err", err)
				}
			}
		}
	}()
	return p.wg.Wait
}

func (p *partitionProcessor) unregisterMetrics() {
	for _, b := range p.builders {
		b.UnregisterMetrics(b.Registerer)
	}
	p.metrics.unregister(p.reg)
	p.uploader.UnregisterMetrics(p.reg)
}

func (p *partitionProcessor) initBuilder() error {
	var initErr error
	p.builderOnce.Do(func() {
		for i := 0; i < 2; i++ {
			// Dataobj builder
			builder, err := logsobj.NewBuilder(p.cfg, p.scratchStore)
			if err != nil {
				initErr = err
				return
			}
			reg := prometheus.WrapRegistererWith(prometheus.Labels{
				"builder": strconv.FormatInt(int64(i), 10),
			}, p.reg)
			if err := builder.RegisterMetrics(reg); err != nil {
				initErr = err
				return
			}
			p.builders = append(p.builders, builderWithReg{builder, reg})
			p.freeBuilders <- builder
		}
	})
	return initErr
}

func (p *partitionProcessor) processRecord(ctx context.Context, record partition.Record) {
	p.metrics.processedRecords.Inc()

	// Update offset metric at the end of processing
	defer p.metrics.updateOffset(record.Offset)

	if record.Timestamp.Before(p.oldestRecord) || p.oldestRecord.IsZero() {
		p.oldestRecord = record.Timestamp
	}

	// Observe processing delay
	p.metrics.observeProcessingDelay(record.Timestamp)

	// Initialize builder if this is the first record
	if err := p.initBuilder(); err != nil {
		level.Error(p.logger).Log("msg", "failed to initialize builder", "err", err)
		return
	}

	if p.currentBuilder == nil {
		p.currentBuilder = <-p.freeBuilders
	}

	tenant := record.TenantID
	stream, err := p.decoder.DecodeWithoutLabels(record.Content)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to decode record", "err", err)
		return
	}

	p.metrics.incAppendsTotal()
	if err := p.currentBuilder.Append(tenant, stream); err != nil {
		if !errors.Is(err, logsobj.ErrBuilderFull) {
			level.Error(p.logger).Log("msg", "failed to append stream", "err", err)
			p.metrics.incAppendFailures()
			return
		}

		p.scheduleFlush(ctx)
		p.currentBuilder = <-p.freeBuilders

		p.metrics.incAppendsTotal()
		if err := p.currentBuilder.Append(tenant, stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
			p.metrics.incAppendFailures()
		}
	}

	p.oldestRecord = record.Timestamp
	p.lastModified = p.clock.Now()
}

// idleFlush flushes the partition if it has exceeded the idle flush timeout.
// It returns true if the partition was flushed, false with a non-nil error
// if the partition could not be flushed, and false with a nil error if
// the partition has not exceeded the timeout.
func (p *partitionProcessor) idleFlush(ctx context.Context) (bool, error) {
	if !p.needsIdleFlush() {
		return false, nil
	}
	p.scheduleFlush(ctx)
	return true, nil
}

// needsIdleFlush returns true if the partition has exceeded the idle timeout
// and the builder has some data buffered.
func (p *partitionProcessor) needsIdleFlush() bool {
	// This is a safety check to make sure we never flush empty data objects.
	// It should never happen that lastModified is non-zero while the builder
	// is either uninitialized or empty.
	if p.currentBuilder == nil || p.currentBuilder.GetEstimatedSize() == 0 {
		return false
	}
	if p.lastModified.IsZero() {
		return false
	}
	return p.clock.Since(p.lastModified) > p.idleFlushTimeout
}

func (p *partitionProcessor) finalFlush(ctx context.Context) error {
	if !p.needsFinalFlush() {
		return nil
	}
	p.scheduleFlush(ctx)
	return nil
}

func (p *partitionProcessor) needsFinalFlush() bool {
	if p.currentBuilder == nil || p.currentBuilder.GetEstimatedSize() == 0 {
		return false
	}
	if p.lastModified.IsZero() {
		return false
	}
	return true
}

func (p *partitionProcessor) scheduleFlush(ctx context.Context) {
	if p.currentBuilder == nil {
		return
	}
	currentBuilder := p.currentBuilder
	p.flusher.Enqueue(ctx, job{
		builder:      currentBuilder,
		offset:       p.currentOffset,
		oldestRecord: p.oldestRecord,
		callbackDone: func() {
			level.Info(p.logger).Log("msg", "returning builder to free list")
			p.freeBuilders <- currentBuilder
			level.Info(p.logger).Log("msg", "builder returned to free list")
		},
	})
	p.currentBuilder = nil
}
