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
	consumeLatency      prometheus.Histogram
	currentOffset       prometheus.Gauge
	pushLatency         prometheus.Histogram
	consumeWorkersCount prometheus.Gauge
	batchSize           prometheus.Histogram
	workerQueueDepth    prometheus.Gauge
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
		consumeWorkersCount: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_partition_consume_workers_count",
			Help: "The number of workers that are processing records from Kafka",
		}),
		batchSize: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_partition_batch_size",
			Help:                        "The size of batches being processed",
			NativeHistogramBucketFactor: 1.1,
		}),
		workerQueueDepth: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_partition_worker_queue_depth",
			Help: "Current depth of worker queue",
		}),
	}
}

func NewKafkaConsumerFactory(pusher logproto.PusherServer, reg prometheus.Registerer, maxConsumerWorkers, maxSuccessBuffer int) partition.ConsumerFactory {
	metrics := newConsumerMetrics(reg)
	metrics.consumeWorkersCount.Set(float64(maxConsumerWorkers))

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
			workerPool:         make(chan struct{}, maxConsumerWorkers),
			workChan:           make(chan recordWithIndex, maxConsumerWorkers*2), // Buffer to reduce contention
			successBuffer:      make([]*int64, 0, maxSuccessBuffer),              // Pre-allocate buffer
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

	// Worker pool management
	workerPool    chan struct{}
	workChan      chan recordWithIndex
	successBuffer []*int64
	poolMutex     sync.Mutex
}

type recordWithIndex struct {
	record partition.Record
	index  int
}

func (kc *kafkaConsumer) Start(ctx context.Context, recordsCh <-chan []partition.Record) func() {
	var wg sync.WaitGroup
	wg.Add(1)

	// Pre-fill worker pool
	for i := 0; i < kc.maxConsumerWorkers; i++ {
		kc.workerPool <- struct{}{}
	}

	go func() {
		defer wg.Done()
		defer close(kc.workChan)

		for {
			select {
			case <-ctx.Done():
				// We have been asked to exit, even if we haven't processed
				// all records. No unprocessed records will be committed.
				level.Info(kc.logger).Log("msg", "stopping, context canceled")
				return
			case records, ok := <-recordsCh:
				if !ok {
					// All records have been processed, we can exit.
					level.Info(kc.logger).Log("msg", "stopping, channel closed")
					return
				}
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

	kc.metrics.batchSize.Observe(float64(len(records)))

	var (
		minOffset    = int64(math.MaxInt64)
		maxOffset    = int64(0)
		consumeStart = time.Now()
		wg           sync.WaitGroup
	)

	// Find min/max offsets
	for _, record := range records {
		minOffset = min(minOffset, record.Offset)
		maxOffset = max(maxOffset, record.Offset)
	}

	level.Debug(kc.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset, "batch_size", len(records))

	// Reuse or resize success buffer
	kc.poolMutex.Lock()
	if cap(kc.successBuffer) < len(records) {
		kc.successBuffer = make([]*int64, len(records))
	} else {
		kc.successBuffer = kc.successBuffer[:len(records)]
		for i := range kc.successBuffer {
			kc.successBuffer[i] = nil
		}
	}
	kc.poolMutex.Unlock()

	kc.metrics.workerQueueDepth.Set(float64(len(kc.workChan)))

	// Distribute work to available workers
	workersLaunched := 0
	for i := range records {
		select {
		case <-ctx.Done():
			level.Info(kc.logger).Log("msg", "context canceled during work distribution")
			return
		case kc.workChan <- recordWithIndex{record: records[i], index: i}:
			// Try to acquire worker from pool
			select {
			case <-kc.workerPool:
				wg.Add(1)
				workersLaunched++
				go kc.worker(ctx, &wg)
			default:
				// No available workers, work will be picked up when workers free up
			}
		}
	}

	// Wait for all work to complete
	wg.Wait()

	// Return workers to pool
	for i := 0; i < workersLaunched; i++ {
		kc.workerPool <- struct{}{}
	}

	// Find the highest offset before a gap, and commit that.
	var highestOffset int64
	for _, offset := range kc.successBuffer {
		if offset == nil || *offset == 0 {
			break
		}
		highestOffset = *offset
	}
	if highestOffset > 0 {
		kc.committer.EnqueueOffset(highestOffset)
	}

	kc.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
	kc.metrics.currentOffset.Set(float64(maxOffset))
}

func (kc *kafkaConsumer) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		// Return worker token to pool when done
		kc.workerPool <- struct{}{}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case recordWithIndex, ok := <-kc.workChan:
			if !ok {
				return
			}
			kc.processRecord(ctx, recordWithIndex)
		default:
			// No more work, exit
			return
		}
	}
}

func (kc *kafkaConsumer) processRecord(ctx context.Context, rwi recordWithIndex) {
	stream, err := kc.decoder.DecodeWithoutLabels(rwi.record.Content)
	if err != nil {
		level.Error(kc.logger).Log("msg", "failed to decode record", "error", err)
		return
	}

	recordCtx := user.InjectOrgID(rwi.record.Ctx, rwi.record.TenantID)
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{stream},
	}

	level.Debug(kc.logger).Log("msg", "pushing record", "offset", rwi.record.Offset, "length", len(rwi.record.Content))

	if err := kc.retryWithBackoff(ctx, func(attempts int) error {
		pushTime := time.Now()
		_, err := kc.pusher.Push(recordCtx, req)

		kc.metrics.pushLatency.Observe(time.Since(pushTime).Seconds())

		if err != nil {
			level.Warn(kc.logger).Log("msg", "failed to push records", "err", err, "offset", rwi.record.Offset, "attempts", attempts)
			return err
		}

		return nil
	}); err != nil {
		level.Error(kc.logger).Log("msg", "exhausted all retry attempts, failed to push records", "err", err, "offset", rwi.record.Offset)
		return
	}

	offset := rwi.record.Offset
	kc.successBuffer[rwi.index] = &offset
}

func (kc *kafkaConsumer) retryWithBackoff(ctx context.Context, fn func(attempts int) error) error {
	err := fn(0)
	if err == nil {
		return nil
	}
	if !canRetry(err) {
		return err
	}

	// Optimized backoff: faster initial retries with exponential slowdown
	backoffCfg := backoff.Config{
		MinBackoff: 50 * time.Millisecond, // Faster initial retry
		MaxBackoff: 10 * time.Second,      // Longer max for persistent issues
		MaxRetries: 0,
	}

	backoff := backoff.New(ctx, backoffCfg)
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

func canRetry(err error) bool {
	return errors.Is(err, ErrReadOnly)
}

// Helper functions for min/max (already present in Go 1.21+)
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
