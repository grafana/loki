package consumer

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka"
)

type partitionProcessor struct {
	// Kafka client and topic/partition info
	client    *kgo.Client
	topic     string
	partition int32
	tenantID  []byte
	// Processing pipeline
	records          chan *kgo.Record
	builder          *logsobj.Builder
	decoder          *kafka.Decoder
	uploader         *uploader.Uploader
	metastoreUpdater *metastore.Updater

	// Builder initialization
	builderOnce sync.Once
	builderCfg  logsobj.BuilderConfig
	bucket      objstore.Bucket
	bufPool     *sync.Pool

	// Idle stream handling
	idleFlushTimeout time.Duration
	// The initial value is the zero time.
	lastFlush    time.Time
	lastModified time.Time

	// Metrics
	metrics *partitionOffsetMetrics

	// Control and coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger

	eventsProducerClient *kgo.Client

	// Used for tests.
	clock quartz.Clock
}

func newPartitionProcessor(
	ctx context.Context,
	client *kgo.Client,
	builderCfg logsobj.BuilderConfig,
	uploaderCfg uploader.Config,
	metastoreCfg metastore.Config,
	bucket objstore.Bucket,
	tenantID string,
	virtualShard int32,
	topic string,
	partition int32,
	logger log.Logger,
	reg prometheus.Registerer,
	bufPool *sync.Pool,
	idleFlushTimeout time.Duration,
	eventsProducerClient *kgo.Client,
) *partitionProcessor {
	ctx, cancel := context.WithCancel(ctx)
	decoder, err := kafka.NewDecoder()
	if err != nil {
		panic(err)
	}
	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"shard":     strconv.Itoa(int(virtualShard)),
		"partition": strconv.Itoa(int(partition)),
		"tenant":    tenantID,
		"topic":     topic,
	}, reg)

	metrics := newPartitionOffsetMetrics()
	if err := metrics.register(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register partition metrics", "err", err)
	}

	uploader := uploader.New(uploaderCfg, bucket, tenantID, logger)
	if err := uploader.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register uploader metrics", "err", err)
	}

	metastoreUpdater := metastore.NewUpdater(metastoreCfg, bucket, tenantID, logger)
	if err := metastoreUpdater.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register metastore updater metrics", "err", err)
	}

	return &partitionProcessor{
		client:               client,
		logger:               log.With(logger, "topic", topic, "partition", partition, "tenant", tenantID),
		topic:                topic,
		partition:            partition,
		records:              make(chan *kgo.Record, 1000),
		ctx:                  ctx,
		cancel:               cancel,
		decoder:              decoder,
		reg:                  reg,
		builderCfg:           builderCfg,
		bucket:               bucket,
		tenantID:             []byte(tenantID),
		metrics:              metrics,
		uploader:             uploader,
		metastoreUpdater:     metastoreUpdater,
		bufPool:              bufPool,
		idleFlushTimeout:     idleFlushTimeout,
		eventsProducerClient: eventsProducerClient,
		clock:                quartz.NewReal(),
	}
}

func (p *partitionProcessor) start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		level.Info(p.logger).Log("msg", "started partition processor")
		for {
			select {
			case <-p.ctx.Done():
				level.Info(p.logger).Log("msg", "stopping partition processor")
				return
			case record, ok := <-p.records:
				if !ok {
					// Channel was closed
					return
				}
				p.processRecord(record)

			case <-time.After(p.idleFlushTimeout):
				if _, err := p.idleFlush(); err != nil {
					level.Error(p.logger).Log("msg", "failed to idle flush", "err", err)
				}
			}
		}
	}()
}

func (p *partitionProcessor) stop() {
	p.cancel()
	p.wg.Wait()
	if p.builder != nil {
		p.builder.UnregisterMetrics(p.reg)
	}
	p.metrics.unregister(p.reg)
	p.uploader.UnregisterMetrics(p.reg)
}

// Drops records from the channel if the processor is stopped.
// Returns false if the processor is stopped, true otherwise.
func (p *partitionProcessor) Append(records []*kgo.Record) bool {
	for _, record := range records {
		select {
		// must check per-record in order to not block on a full channel
		// after receiver has been stopped.
		case <-p.ctx.Done():
			return false
		case p.records <- record:
		}
	}
	return true
}

func (p *partitionProcessor) initBuilder() error {
	var initErr error
	p.builderOnce.Do(func() {
		// Dataobj builder
		builder, err := logsobj.NewBuilder(p.builderCfg)
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

func (p *partitionProcessor) flush() error {
	minTime, maxTime := p.builder.TimeRange()

	obj, closer, err := p.builder.Flush()
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
		return err
	}
	defer closer.Close()

	objectPath, err := p.uploader.Upload(p.ctx, obj)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to upload object", "err", err)
		return err
	}

	if err := p.metastoreUpdater.Update(p.ctx, objectPath, minTime, maxTime); err != nil {
		level.Error(p.logger).Log("msg", "failed to update metastore", "err", err)
		return err
	}

	if err := p.emitObjectWrittenEvent(objectPath); err != nil {
		level.Error(p.logger).Log("msg", "failed to emit event", "err", err)
		return err
	}

	p.lastFlush = p.clock.Now()

	return nil
}

func (p *partitionProcessor) emitObjectWrittenEvent(objectPath string) error {
	if p.eventsProducerClient == nil {
		return nil
	}

	event := &metastore.ObjectWrittenEvent{
		Tenant:     string(p.tenantID),
		ObjectPath: objectPath,
		WriteTime:  p.clock.Now().Format(time.RFC3339),
	}

	eventBytes, err := event.Marshal()
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to marshal metastore event", "err", err)
		return err
	}

	// Emitting the event is non-critical so we don't need to wait for it.
	// We can just log the error and move on.
	p.eventsProducerClient.Produce(p.ctx, &kgo.Record{
		Value: eventBytes,
	}, func(_ *kgo.Record, err error) {
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to produce metastore event", "err", err)
		}
	})

	return nil
}

func (p *partitionProcessor) processRecord(record *kgo.Record) {
	// Update offset metric at the end of processing
	defer p.metrics.updateOffset(record.Offset)

	// Observe processing delay
	p.metrics.observeProcessingDelay(record.Timestamp)

	// Initialize builder if this is the first record
	if err := p.initBuilder(); err != nil {
		level.Error(p.logger).Log("msg", "failed to initialize builder", "err", err)
		return
	}

	// todo: handle multi-tenant
	if !bytes.Equal(record.Key, p.tenantID) {
		level.Error(p.logger).Log("msg", "record key does not match tenant ID", "key", record.Key, "tenant_id", p.tenantID)
		return
	}
	stream, err := p.decoder.DecodeWithoutLabels(record.Value)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to decode record", "err", err)
		return
	}

	p.metrics.incAppendsTotal()
	if err := p.builder.Append(stream); err != nil {
		if !errors.Is(err, logsobj.ErrBuilderFull) {
			level.Error(p.logger).Log("msg", "failed to append stream", "err", err)
			p.metrics.incAppendFailures()
			return
		}

		if err := p.flush(); err != nil {
			level.Error(p.logger).Log("msg", "failed to flush stream", "err", err)
			return
		}

		if err := p.commitRecords(record); err != nil {
			level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
			return
		}

		p.metrics.incAppendsTotal()
		if err := p.builder.Append(stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
			p.metrics.incAppendFailures()
		}
	}

	p.lastModified = p.clock.Now()
}

func (p *partitionProcessor) commitRecords(record *kgo.Record) error {
	backoff := backoff.New(p.ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(p.ctx, record)
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
func (p *partitionProcessor) idleFlush() (bool, error) {
	if !p.needsIdleFlush() {
		return false, nil
	}
	if err := p.flush(); err != nil {
		return false, err
	}
	return true, nil
}

// isIdle returns true if the partition has exceeded the idle flush timeout.
func (p *partitionProcessor) needsIdleFlush() bool {
	if p.builder == nil {
		return false
	}
	if p.lastModified.IsZero() {
		return false
	}
	return p.clock.Since(p.lastModified) > p.idleFlushTimeout
}
