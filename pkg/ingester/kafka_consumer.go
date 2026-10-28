package ingester

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
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
	}
}

func NewKafkaConsumerFactory(pusher logproto.PusherServer, reg prometheus.Registerer, maxConsumerWorkers int) partition.ConsumerFactory {
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

func (kc *kafkaConsumer) Start(ctx context.Context, recordsCh <-chan []partition.Record) func() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
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
	var (
		minOffset    = int64(math.MaxInt64)
		maxOffset    = int64(0)
		consumeStart = time.Now()
		limitWorkers = kc.maxConsumerWorkers
		wg           sync.WaitGroup
	)

	// Find min/max offsets
	for _, record := range records {
		minOffset = min(minOffset, record.Offset)
		maxOffset = max(maxOffset, record.Offset)
	}

	level.Debug(kc.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset)

	// Each worker applies all the records of a stream, in offset order. The order a stream's
	// entries arrive in decides which of them share a chunk, so replicas of the partition only
	// build identical chunks, which the chunk store can then dedupe, if they apply its records
	// in the same order. Different streams are still applied in parallel.
	groups := groupByStream(records)
	numWorkers := min(limitWorkers, len(groups))
	workChan := make(chan []int, len(groups))
	for _, group := range groups {
		workChan <- group.indexes
	}
	close(workChan)
	// success keeps track of the records that were processed. It is expected to
	// be sorted in ascending order of offset since the records themselves are
	// ordered.
	success := make([]*int64, len(records))

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for indexes := range workChan {
				for _, index := range indexes {
					record := records[index]
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

					offset := record.Offset
					success[index] = &offset
				}
			}
		}()
	}

	wg.Wait()

	// Find the highest offset before a gap, and commit that.
	var highestOffset int64
	for _, offset := range success {
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

// streamGroup is the records of a batch that belong to one stream.
type streamGroup struct {
	indexes []int // indexes of the records in the batch, in offset order.
	size    int   // total size of the records.
}

// groupByStream groups the records of a batch by their tenant and stream labels, largest group
// first so the workers start on the longest streams. A record whose labels can't be read still
// joins a group, and fails to decode when it is applied.
//
// Groups are keyed on a hash of the tenant and labels. Two streams that collide share a group,
// which only means they are applied one after the other.
func groupByStream(records []partition.Record) []streamGroup {
	var (
		digest = xxhash.New()
		byKey  = make(map[uint64]int, len(records))
		groups []streamGroup
	)
	for i, record := range records {
		labels, _ := kafka.StreamLabels(record.Content)
		digest.Reset()
		_, _ = digest.WriteString(record.TenantID)
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(labels)
		key := digest.Sum64()

		g, ok := byKey[key]
		if !ok {
			g = len(groups)
			byKey[key] = g
			groups = append(groups, streamGroup{})
		}
		groups[g].indexes = append(groups[g].indexes, i)
		groups[g].size += len(record.Content)
	}
	slices.SortStableFunc(groups, func(a, b streamGroup) int {
		return cmp.Compare(b.size, a.size)
	})
	return groups
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
