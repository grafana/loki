package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
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
	CopyAndSort(obj *dataobj.Object) (*dataobj.Object, io.Closer, error)
}

// committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, partition int32, offset int64) error
}

type producer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

type partitionProcessor struct {
	// Kafka client and topic/partition info
	committer committer
	topic     string
	partition int32
	// lastRecord contains the last record appended to the builder. It is used
	// to commit the correct offset after a flush.
	lastRecord *kgo.Record
	builder    builder
	decoder    *kafka.Decoder
	uploader   *uploader.Uploader

	// Builder initialization
	builderOnce  sync.Once
	builderCfg   logsobj.BuilderConfig
	bucket       objstore.Bucket
	scratchStore scratch.Store

	// Idle stream handling
	idleFlushTimeout time.Duration
	// The initial value is the zero time.
	lastFlushed time.Time
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
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger

	eventsProducerClient    producer
	metastorePartitionRatio int32

	recordsChan chan *kgo.Record

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
	maxBuilderAge time.Duration,
	eventsProducerClient *kgo.Client,
	topic string,
	partition int32,
	recordsChan chan *kgo.Record,
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
		topic:                   topic,
		partition:               partition,
		committer:               committer,
		logger:                  logger,
		decoder:                 decoder,
		reg:                     reg,
		builderCfg:              builderCfg,
		bucket:                  bucket,
		scratchStore:            scratchStore,
		metrics:                 metrics,
		uploader:                uploader,
		idleFlushTimeout:        idleFlushTimeout,
		maxBuilderAge:           maxBuilderAge,
		eventsProducerClient:    eventsProducerClient,
		clock:                   quartz.NewReal(),
		metastorePartitionRatio: int32(metastoreCfg.PartitionRatio),
		recordsChan:             recordsChan,
	}
}

func (p *partitionProcessor) Start(ctx context.Context) func() {
	// This is a hack to avoid duplicate metrics registration panics. The
	// problem occurs because [kafka.ReaderService] creates a consumer to
	// process lag on startup, tears it down, and then creates another one
	// once the lag threshold has been met. The new [kafkav2] package will
	// solve this by de-coupling the consumer from processing consumer lag.
	p.wg.Add(1)
	go func() {
		defer func() {
			level.Info(p.logger).Log("msg", "stopped partition processor")
			p.wg.Done()
		}()
		level.Info(p.logger).Log("msg", "started partition processor")
		for {
			select {
			case <-ctx.Done():
				level.Info(p.logger).Log("msg", "stopping partition processor, context canceled")
				return
			case record, ok := <-p.recordsChan:
				if !ok {
					level.Info(p.logger).Log("msg", "stopping partition processor, channel closed")
					return
				}
				p.processRecord(ctx, record)
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

func (p *partitionProcessor) emitObjectWrittenEvent(ctx context.Context, objectPath string) error {
	event := &metastore.ObjectWrittenEvent{
		ObjectPath:         objectPath,
		WriteTime:          p.clock.Now().Format(time.RFC3339),
		EarliestRecordTime: p.earliestRecordTime.Format(time.RFC3339),
	}

	eventBytes, err := event.Marshal()
	if err != nil {
		return err
	}

	// Apply the partition ratio to the incoming partition to find the metastore topic partition.
	// This has the effect of concentrating the log partitions to fewer metastore partitions for later processing.
	partition := p.partition / p.metastorePartitionRatio

	results := p.eventsProducerClient.ProduceSync(ctx, &kgo.Record{
		Partition: partition,
		Value:     eventBytes,
	})
	return results.FirstErr()
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
		if err := p.flushAndCommit(ctx); err != nil {
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
		if err := p.flushAndCommit(ctx); err != nil {
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

// flushAndCommit flushes the builder and, if successful, commits the offset
// of the last record processed. It expects that the last record processed
// was also the last record appended to the builder. If not, data loss can
// occur should the consumer restart or a partition rebalance occur
func (p *partitionProcessor) flushAndCommit(ctx context.Context) error {
	if err := p.flush(ctx); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	if err := p.commit(ctx); err != nil {
		return fmt.Errorf("failed to commit offset: %w", err)
	}
	return nil
}

// flush builds a complete data object from the builder, uploads it, records
// it in the metastore, and emits an object written event to the events topic.
func (p *partitionProcessor) flush(ctx context.Context) error {
	timer := prometheus.NewTimer(p.metrics.flushDuration)
	defer timer.ObserveDuration()

	// The time range must be read before the flush as the builder is reset
	// at the end of each flush, resetting the time range.
	obj, closer, err := p.builder.Flush()
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
		return err
	}

	obj, closer, err = p.sort(obj, closer)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to sort dataobj", "err", err)
		return err
	}
	defer closer.Close()

	objectPath, err := p.uploader.Upload(ctx, obj)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to upload object", "err", err)
		return err
	}

	if err := p.emitObjectWrittenEvent(ctx, objectPath); err != nil {
		level.Error(p.logger).Log("msg", "failed to emit metastore event", "err", err)
		return err
	}

	p.lastModified = time.Time{}
	p.lastFlushed = p.clock.Now()
	p.earliestRecordTime = time.Time{}
	p.firstAppendTime = time.Time{}

	return nil
}

func (p *partitionProcessor) sort(obj *dataobj.Object, closer io.Closer) (*dataobj.Object, io.Closer, error) {
	defer closer.Close()

	start := time.Now()
	defer func() {
		level.Debug(p.logger).Log("msg", "partition processor sorted logs object-wide", "duration", time.Since(start))
	}()

	return p.builder.CopyAndSort(obj)
}

// commits the offset of the last record processed. It should be called after
// each successful flush to avoid duplicate data in consecutive data objects.
func (p *partitionProcessor) commit(ctx context.Context) error {
	if p.lastRecord == nil {
		return errors.New("failed to commit offset, no records processed")
	}
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})
	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		level.Debug(p.logger).Log("msg", "committed offset", "partition", p.partition, "offset", p.lastRecord.Offset)
		err := p.committer.Commit(ctx, p.partition, p.lastRecord.Offset)
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
		p.metrics.incCommitFailures()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
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
	if err := p.flushAndCommit(ctx); err != nil {
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
