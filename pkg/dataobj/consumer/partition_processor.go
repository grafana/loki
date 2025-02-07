package consumer

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
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
	builder          *dataobj.Builder
	decoder          *kafka.Decoder
	uploader         *uploader.Uploader
	metastoreManager *metastore.Manager

	// Builder initialization
	builderOnce sync.Once
	builderCfg  dataobj.BuilderConfig
	bucket      objstore.Bucket
	bufPool     *sync.Pool

	// Metrics
	metrics *partitionOffsetMetrics

	// Control and coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger
}

func newPartitionProcessor(ctx context.Context, client *kgo.Client, builderCfg dataobj.BuilderConfig, uploaderCfg uploader.Config, bucket objstore.Bucket, tenantID string, virtualShard int32, topic string, partition int32, logger log.Logger, reg prometheus.Registerer, bufPool *sync.Pool) *partitionProcessor {
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

	uploader := uploader.New(uploaderCfg, bucket, tenantID)
	if err := uploader.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register uploader metrics", "err", err)
	}

	metastoreManager := metastore.NewManager(bucket, tenantID, logger)
	if err := metastoreManager.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register metastore manager metrics", "err", err)
	}

	return &partitionProcessor{
		client:           client,
		logger:           log.With(logger, "topic", topic, "partition", partition, "tenant", tenantID),
		topic:            topic,
		partition:        partition,
		records:          make(chan *kgo.Record, 1000),
		ctx:              ctx,
		cancel:           cancel,
		decoder:          decoder,
		reg:              reg,
		builderCfg:       builderCfg,
		bucket:           bucket,
		tenantID:         []byte(tenantID),
		metrics:          metrics,
		uploader:         uploader,
		metastoreManager: metastoreManager,
		bufPool:          bufPool,
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
		builder, err := dataobj.NewBuilder(p.builderCfg)
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

	if err := p.builder.Append(stream); err != nil {
		if err != dataobj.ErrBuilderFull {
			level.Error(p.logger).Log("msg", "failed to append stream", "err", err)
			p.metrics.incAppendFailures()
			return
		}

		func() {
			flushBuffer := p.bufPool.Get().(*bytes.Buffer)
			defer p.bufPool.Put(flushBuffer)

			flushBuffer.Reset()

			flushedDataobjStats, err := p.builder.Flush(flushBuffer)
			if err != nil {
				level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
				return
			}

			objectPath, err := p.uploader.Upload(p.ctx, flushBuffer)
			if err != nil {
				level.Error(p.logger).Log("msg", "failed to upload object", "err", err)
				return
			}

			if err := p.metastoreManager.UpdateMetastore(p.ctx, objectPath, flushedDataobjStats); err != nil {
				level.Error(p.logger).Log("msg", "failed to update metastore", "err", err)
				return
			}
		}()

		if err := p.commitRecords(record); err != nil {
			level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
			return
		}

		if err := p.builder.Append(stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
			p.metrics.incAppendFailures()
		}
	}
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
