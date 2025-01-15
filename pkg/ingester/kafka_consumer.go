package ingester

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
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

func NewKafkaConsumerFactory(pusher logproto.PusherServer, reg prometheus.Registerer) partition.ConsumerFactory {
	metrics := newConsumerMetrics(reg)
	return func(committer partition.Committer, logger log.Logger) (partition.Consumer, error) {
		decoder, err := kafka.NewDecoder()
		if err != nil {
			return nil, err
		}
		return &kafkaConsumer{
			pusher:    pusher,
			logger:    logger,
			decoder:   decoder,
			metrics:   metrics,
			committer: committer,
		}, nil
	}
}

type kafkaConsumer struct {
	pusher    logproto.PusherServer
	logger    log.Logger
	decoder   *kafka.Decoder
	committer partition.Committer

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
				// It can happen that the context is canceled while there are unprocessed records
				// in the channel. However, we do not need to process all remaining records,
				// and can exit out instead, as partition offsets are not committed until
				// the record has been handed over to the Pusher and committed in the WAL.
				level.Info(kc.logger).Log("msg", "shutting down kafka consumer")
				return
			case records := <-recordsChan:
				kc.consume(ctx, records)
			}
		}
	}()
	return wg.Wait
}

func (kc *kafkaConsumer) consume(ctx context.Context, records []partition.Record) {
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
		recordCtx := user.InjectOrgID(record.Ctx, record.TenantID)
		req := &logproto.PushRequest{
			Streams: []logproto.Stream{stream},
		}
		if err := retryWithBackoff(ctx, func(attempts int) error {
			if _, err := kc.pusher.Push(recordCtx, req); err != nil {
				level.Warn(kc.logger).Log("msg", "failed to push records", "err", err, "offset", record.Offset, "attempts", attempts)
				return err
			}
			return nil
		}); err != nil {
			level.Error(kc.logger).Log("msg", "exhausted all retry attempts, failed to push records", "err", err, "offset", record.Offset)
		}
		kc.committer.EnqueueOffset(record.Offset)
	}
	kc.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
	kc.metrics.currentOffset.Set(float64(maxOffset))
}

func canRetry(err error) bool {
	return errors.Is(err, ErrReadOnly)
}

func retryWithBackoff(ctx context.Context, fn func(attempts int) error) error {
	err := fn(0)
	if err == nil {
		return nil
	}
	if !canRetry(err) {
		return err
	}
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: 0, // Retry infinitely
	})
	backoff.Wait()
	for backoff.Ongoing() {
		err = fn(backoff.NumRetries())
		if err == nil {
			return nil
		}
		if !canRetry(err) {
			return err
		}
		backoff.Wait()
	}
	return backoff.Err()
}
