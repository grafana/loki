package ingester

import (
	"context"
	math "math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type consumerMetrics struct {
	consumeLatency prometheus.Histogram
	currentOffset  prometheus.Gauge
}

// newConsumerMetrics initializes and returns a new consumerMetrics instance
func newConsumerMetrics(reg prometheus.Registerer) *consumerMetrics {
	return &consumerMetrics{
		consumeLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_partition_records_batch_process_duration_seconds",
			Help:                        "How long a kafka ingester consumer spent processing a batch of records from Kafka.",
			NativeHistogramBucketFactor: 1.1,
		}),
		currentOffset: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_partition_current_offset",
			Help: "The current offset of the Kafka ingester consumer.",
		}),
	}
}

func NewKafkaConsumerFactory(pusher logproto.PusherServer, logger log.Logger, reg prometheus.Registerer) partition.ConsumerFactory {
	metrics := newConsumerMetrics(reg)
	return func(committer partition.Committer) (partition.Consumer, error) {
		decoder, err := kafka.NewDecoder()
		if err != nil {
			return nil, err
		}
		return &kafkaConsumer{
			pusher:  pusher,
			logger:  logger,
			decoder: decoder,
			metrics: metrics,
		}, nil
	}
}

type kafkaConsumer struct {
	pusher  logproto.PusherServer
	logger  log.Logger
	decoder *kafka.Decoder

	metrics *consumerMetrics
}

func (kc *kafkaConsumer) Start(ctx context.Context, recordsChan <-chan []partition.Record) func() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				level.Info(kc.logger).Log("msg", "shutting down kafka consumer")
				return
			case records := <-recordsChan:
				kc.consume(records)
			}
		}
	}()
	return wg.Wait
}

func (kc *kafkaConsumer) consume(records []partition.Record) {
	if len(records) == 0 {
		return
	}
	var (
		minOffset    = int64(math.MaxInt64)
		maxOffset    = int64(0)
		consumeStart = time.Now()
	)

	for _, record := range records {
		minOffset = min(minOffset, record.Offset)
		maxOffset = max(maxOffset, record.Offset)
	}

	level.Debug(kc.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset)
	for _, record := range records {
		stream, err := kc.decoder.DecodeWithoutLabels(record.Content)
		if err != nil {
			level.Error(kc.logger).Log("msg", "failed to decode record", "error", err)
			continue
		}
		ctx := user.InjectOrgID(record.Ctx, record.TenantID)
		if _, err := kc.pusher.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{stream},
		}); err != nil {
			level.Error(kc.logger).Log("msg", "failed to push records", "error", err)
		}
	}
	kc.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
	kc.metrics.currentOffset.Set(float64(maxOffset))
}
