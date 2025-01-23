package consumer

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	metastoreWindowSize = 12 * time.Hour
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
	metastoreBuilder *dataobj.Builder
	decoder          *kafka.Decoder

	// Builder initialization
	builderOnce sync.Once
	builderCfg  dataobj.BuilderConfig
	bucket      objstore.Bucket

	// Metrics
	metrics *partitionOffsetMetrics

	// Control and coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger
}

func newPartitionProcessor(ctx context.Context, client *kgo.Client, builderCfg dataobj.BuilderConfig, bucket objstore.Bucket, tenantID string, topic string, partition int32, logger log.Logger, reg prometheus.Registerer) *partitionProcessor {
	ctx, cancel := context.WithCancel(ctx)
	decoder, err := kafka.NewDecoder()
	if err != nil {
		panic(err)
	}
	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"partition": strconv.Itoa(int(partition)),
	}, reg)

	metrics := newPartitionOffsetMetrics()
	if err := metrics.register(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register partition metrics", "err", err)
	}

	return &partitionProcessor{
		client:     client,
		logger:     log.With(logger, "component", "dataobj-consumer", "topic", topic, "partition", partition),
		topic:      topic,
		partition:  partition,
		records:    make(chan *kgo.Record, 1000),
		ctx:        ctx,
		cancel:     cancel,
		decoder:    decoder,
		reg:        reg,
		builderCfg: builderCfg,
		bucket:     bucket,
		tenantID:   []byte(tenantID),
		metrics:    metrics,
	}
}

func (p *partitionProcessor) start() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer close(p.records)

		level.Info(p.logger).Log("msg", "started partition processor")
		for {
			select {
			case <-p.ctx.Done():
				level.Info(p.logger).Log("msg", "stopping partition processor")
				return
			case record := <-p.records:
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
}

func (p *partitionProcessor) initBuilder() error {
	var initErr error
	p.builderOnce.Do(func() {
		builder, err := dataobj.NewBuilder(p.builderCfg, p.bucket, string(p.tenantID))
		if err != nil {
			initErr = err
			return
		}
		if err := builder.RegisterMetrics(p.reg); err != nil {
			initErr = err
			return
		}
		p.builder = builder

		metastoreBuilder, err := dataobj.NewBuilder(p.builderCfg, p.bucket, string(p.tenantID))
		if err != nil {
			initErr = err
			return
		}
		p.metastoreBuilder = metastoreBuilder
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
		return
	}
	stream, err := p.decoder.DecodeWithoutLabels(record.Value)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to decode record", "err", err)
		return
	}

	if err := p.builder.Append(stream); err != nil {
		if err != dataobj.ErrBufferFull {
			level.Error(p.logger).Log("msg", "failed to append stream", "err", err)
			p.metrics.incAppendFailures()
			return
		}

		backoff := backoff.New(p.ctx, backoff.Config{
			MinBackoff: 100 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		})

		var dataobjPath string
		for backoff.Ongoing() {
			dataobjPath, err = p.builder.Flush(p.ctx)
			if err == nil {
				break
			}
			level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
			p.metrics.incFlushFailures()
			backoff.Wait()
		}

		err = p.writeMetastores(backoff, dataobjPath)
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to write metastores", "err", err)
			return
		}

		// Reset builder after flushing & storing in metastore
		p.builder.Reset()

		backoff.Reset()
		for backoff.Ongoing() {
			err = p.client.CommitRecords(p.ctx, record)
			if err == nil {
				break
			}
			level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
			p.metrics.incCommitFailures()
			backoff.Wait()
		}

		if err := p.builder.Append(stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
			p.metrics.incAppendFailures()
		}
	}
}

func (p *partitionProcessor) writeMetastores(backoff *backoff.Backoff, dataobjPath string) error {
	start := time.Now()
	defer p.metrics.observeMetastoreProcessing(start)
	minTimestamp, maxTimestamp := p.builder.GetStreamMinMax()

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	minMetastoreWindow := minTimestamp.Truncate(metastoreWindowSize)
	maxMetastoreWindow := maxTimestamp.Truncate(metastoreWindowSize)
	for metastoreWindow := minMetastoreWindow; metastoreWindow.Compare(maxMetastoreWindow) <= 0; metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
		metastorePath := fmt.Sprintf("tenant-%s/metastore/%s.store", p.tenantID, metastoreWindow.Format(time.RFC3339))
		backoff.Reset()
		for backoff.Ongoing() {
			err := p.bucket.GetAndReplace(p.ctx, metastorePath, func(existing io.ReadCloser) (io.Reader, error) {
				buf, err := io.ReadAll(existing)
				if err != nil {
					return nil, err
				}

				p.metastoreBuilder.Reset()

				if len(buf) > 0 {
					replayStart := time.Now()
					err = p.metastoreBuilder.FromExisting(bytes.NewReader(buf))
					if err != nil {
						return nil, err
					}
					p.metrics.observeMetastoreReplay(replayStart)
				}

				encodingStart := time.Now()
				p.builder.ForEachStream(func(stream streams.Stream) {
					if stream.MinTimestamp.After(metastoreWindow.Add(metastoreWindowSize)) || stream.MaxTimestamp.Before(metastoreWindow) {
						return
					}
					p.metastoreBuilder.AppendEntries(stream, []logproto.Entry{{
						Line: dataobjPath,
					}})
				})
				newMetastore, err := p.metastoreBuilder.FlushToBuffer()
				if err != nil {
					return nil, err
				}
				p.metrics.observeMetastoreEncoding(encodingStart)
				return newMetastore, nil
			})
			if err == nil {
				level.Info(p.logger).Log("msg", "successfully merged & updated metastore", "store", metastorePath)
				break
			}
			level.Error(p.logger).Log("msg", "failed to get and replace metastore object", "err", err)
			p.metrics.incMetastoreWriteFailures()
			backoff.Wait()
		}
	}
	return nil
}
