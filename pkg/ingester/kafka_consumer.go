package ingester

import (
	"context"
	"errors"
	"math"
	"runtime"
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
	pushLatency    prometheus.Histogram
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
		pushLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_partition_push_latency_seconds",
			Help:                        "The latency of a push request after consuming from Kafka",
			NativeHistogramBucketFactor: 1.1,
		}),
	}
}

func NewKafkaConsumerFactory(pusher logproto.PusherServer, reg prometheus.Registerer, maxConsumerWorkers int) partition.ConsumerFactory {
	metrics := newConsumerMetrics(reg)
	return func(committer partition.Committer, logger log.Logger) (partition.Consumer, error) {
		decoder, err := kafka.NewDecoder()
		if err != nil {
			return nil, err
		}
		return &kafkaConsumer{
			pusher:             pusher,
			logger:             logger,
			decoder:            decoder,
			metrics:            metrics,
			committer:          committer,
			maxConsumerWorkers: maxConsumerWorkers,
		}, nil
	}
}

type kafkaConsumer struct {
	pusher             logproto.PusherServer
	logger             log.Logger
	decoder            *kafka.Decoder
	committer          partition.Committer
	maxConsumerWorkers int
	metrics            *consumerMetrics
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
		limitWorkers = kc.maxConsumerWorkers
		wg           sync.WaitGroup
	)

	// If the number of workers is set to 0, use the number of available CPUs
	if limitWorkers == 0 {
		limitWorkers = runtime.GOMAXPROCS(0)
	}

	// Find min/max offsets
	for _, record := range records {
		minOffset = min(minOffset, record.Offset)
		maxOffset = max(maxOffset, record.Offset)
	}

	level.Debug(kc.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset)

	numWorkers := min(limitWorkers, len(records))
	workChan := make(chan partition.Record, numWorkers)
	// success keeps track of the records that were processed. It is expected to
	// be sorted in ascending order of offset since the records themselves are
	// ordered.
	success := make([]int64, len(records))

	for i := range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for record := range workChan {
				level.Debug(kc.logger).Log("msg", "record", record)
				stream, err := kc.decoder.DecodeWithoutLabels(record.Content)
				if err != nil {
					level.Error(kc.logger).Log("msg", "failed to decode record", "error", err)
					continue
				}

				recordCtx := user.InjectOrgID(record.Ctx, record.TenantID)
				req := &logproto.PushRequest{
					Streams: []logproto.Stream{stream},
				}

				level.Debug(kc.logger).Log("msg", "pushing record", "offset", record.Offset, "length", len(record.Content))

				if err := retryWithBackoff(ctx, func(attempts int) error {
					pushTime := time.Now()
					_, err := kc.pusher.Push(recordCtx, req)

					kc.metrics.pushLatency.Observe(time.Since(pushTime).Seconds())

					if err != nil {
						level.Warn(kc.logger).Log("msg", "failed to push records", "err", err, "offset", record.Offset, "attempts", attempts)
						return err
					}
					return nil
				}); err != nil {
					level.Error(kc.logger).Log("msg", "exhausted all retry attempts, failed to push records", "err", err, "offset", record.Offset)
					continue
				}

				success[i] = record.Offset
			}
		}()
	}

	for _, record := range records {
		workChan <- record
	}
	close(workChan)

	wg.Wait()

	// Find the highest offset before a gap, and commit that.
	var highestOffset int64
	for _, offset := range success {
		if offset == 0 {
			break
		}
		highestOffset = offset
	}
	if highestOffset > 0 {
		kc.committer.EnqueueOffset(highestOffset)
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
