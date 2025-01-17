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
	"github.com/grafana/loki/v3/pkg/kafka"
)

// For now, assume a single tenant
var tenantID = []byte("fake")

type partitionProcessor struct {
	// Kafka client and topic/partition info
	client    *kgo.Client
	topic     string
	partition int32

	// Processing pipeline
	records chan *kgo.Record
	builder *dataobj.Builder
	decoder *kafka.Decoder

	// Control and coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger
}

func newPartitionProcessor(ctx context.Context, client *kgo.Client, builderCfg dataobj.BuilderConfig, bucket objstore.Bucket, topic string, partition int32, logger log.Logger, reg prometheus.Registerer) *partitionProcessor {
	ctx, cancel := context.WithCancel(ctx)
	decoder, err := kafka.NewDecoder()
	if err != nil {
		panic(err)
	}
	builder, err := dataobj.NewBuilder(builderCfg, bucket, string(tenantID))
	if err != nil {
		panic(err)
	}
	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"partition": strconv.Itoa(int(partition)),
	}, reg)
	err = builder.RegisterMetrics(reg)
	if err != nil {
		panic(err)
	}
	return &partitionProcessor{
		client:    client,
		logger:    log.With(logger, "topic", topic, "partition", partition),
		topic:     topic,
		partition: partition,
		records:   make(chan *kgo.Record, 1000),
		ctx:       ctx,
		cancel:    cancel,
		builder:   builder,
		decoder:   decoder,
		reg:       reg,
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
	p.builder.UnregisterMetrics(p.reg)
}

func (p *partitionProcessor) processRecord(record *kgo.Record) {
	// todo: handle multi-tenant
	if !bytes.Equal(record.Key, tenantID) {
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
			return
		}

		backoff := backoff.New(p.ctx, backoff.Config{
			MinBackoff: 100 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		})

		for backoff.Ongoing() {
			err = p.builder.Flush(p.ctx)
			if err == nil {
				break
			}
			level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
			backoff.Wait()
		}

		backoff.Reset()
		for backoff.Ongoing() {
			err = p.client.CommitRecords(p.ctx, record)
			if err == nil {
				break
			}
			level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
			backoff.Wait()
		}

		if err := p.builder.Append(stream); err != nil {
			level.Error(p.logger).Log("msg", "failed to append stream after flushing", "err", err)
		}
	}
}
