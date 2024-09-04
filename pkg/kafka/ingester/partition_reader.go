package ingester

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
)

type PartitionReader struct {
	services.Service

	kafkaCfg        kafka.Config
	partitionID     int32
	consumerGroup   string
	consumer        Consumer
	committer       *partitionCommitter
	lastFetchOffset int64

	client  *kgo.Client
	logger  log.Logger
	metrics readerMetrics
	reg     prometheus.Registerer
}

type record struct {
	// Context holds the tracing (and potentially other) info, that the record was enriched with on fetch from Kafka.
	ctx      context.Context
	tenantID string
	content  []byte
}

type Consumer interface {
	Consume(ctx context.Context, partitionID int32, records []record) error
	Flush(ctx context.Context) error
}

func NewPartitionReader(kafkaCfg kafka.Config, partitionID int32, instanceID string, consumer Consumer, logger log.Logger, reg prometheus.Registerer) (*PartitionReader, error) {
	r := &PartitionReader{
		kafkaCfg:        kafkaCfg,
		partitionID:     partitionID,
		consumerGroup:   kafkaCfg.GetConsumerGroup(instanceID, partitionID),
		consumer:        consumer,
		logger:          logger,
		metrics:         newReaderMetrics(reg),
		reg:             reg,
		lastFetchOffset: -1,
	}
	r.Service = services.NewBasicService(r.start, r.run, nil)
	return r, nil
}

func (p *PartitionReader) start(_ context.Context) error {
	var err error
	p.client, err = kafka.NewReaderClient(p.kafkaCfg, p.metrics.kprom, p.logger,
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			p.kafkaCfg.Topic: {p.partitionID: kgo.NewOffset().AtStart()},
		}),
	)
	if err != nil {
		return errors.Wrap(err, "creating kafka reader client")
	}
	p.committer = newPartitionCommitter(p.kafkaCfg, kadm.NewClient(p.client), p.partitionID, p.consumerGroup, p.logger, p.reg)
	return nil
}

func (p *PartitionReader) run(ctx context.Context) error {
	// todo(cyriltovena): make it configurable
	flushTicker := time.NewTicker(10 * time.Second)
	defer flushTicker.Stop()

	level.Info(p.logger).Log("msg", "starting partition reader", "partition", p.partitionID, "consumer_group", p.consumerGroup)

	// initial fetch
	if err := p.processNextFetches(ctx, p.metrics.receiveDelayWhenStarting); err != nil {
		return err
	}

	for {
		select {
		case <-flushTicker.C:
			err := p.flushAndCommit(context.Background())
			if err != nil {
				return err
			}
			// commit offset
		case <-ctx.Done():
			err := p.flushAndCommit(context.Background())
			if err != nil {
				return err
			}
			return nil
		default:
			err := p.processNextFetches(ctx, p.metrics.receiveDelayWhenRunning)
			if err != nil && !errors.Is(err, context.Canceled) {
				// Fail the whole service in case of a non-recoverable error.
				return err
			}
		}
	}
}

func (r *PartitionReader) flushAndCommit(ctx context.Context) error {
	if r.lastFetchOffset == -1 {
		return nil
	}
	err := r.consumer.Flush(ctx)
	if err != nil {
		return err
	}
	if err := r.committer.commit(ctx, r.lastFetchOffset); err != nil {
		return err
	}
	r.lastFetchOffset = -1
	return nil
}

func (r *PartitionReader) getLastFetchOffset(fetches kgo.Fetches) int64 {
	lastOffset := int64(0)
	fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
		if partition.Partition != r.partitionID {
			level.Error(r.logger).Log("msg", "asked to commit wrong partition", "partition", partition.Partition, "expected_partition", r.partitionID)
			return
		}
		lastOffset = partition.Records[len(partition.Records)-1].Offset
	})
	return lastOffset
}

func (r *PartitionReader) processNextFetches(ctx context.Context, delayObserver prometheus.Observer) error {
	fetches := r.pollFetches(ctx)
	r.recordFetchesMetrics(fetches, delayObserver)
	r.logFetchErrors(fetches)
	fetches = filterOutErrFetches(fetches)

	err := r.consumeFetches(ctx, fetches)
	if err != nil {
		return fmt.Errorf("consume %d records: %w", fetches.NumRecords(), err)
	}
	r.lastFetchOffset = r.getLastFetchOffset(fetches)
	return nil
}

func (r *PartitionReader) consumeFetches(ctx context.Context, fetches kgo.Fetches) error {
	if fetches.NumRecords() == 0 {
		return nil
	}
	records := make([]record, 0, fetches.NumRecords())

	var (
		minOffset = math.MaxInt
		maxOffset = 0
	)
	fetches.EachRecord(func(rec *kgo.Record) {
		minOffset = min(minOffset, int(rec.Offset))
		maxOffset = max(maxOffset, int(rec.Offset))
		records = append(records, record{
			// This context carries the tracing data for this individual record;
			// kotel populates this data when it fetches the messages.
			ctx:      rec.Context,
			tenantID: string(rec.Key),
			content:  rec.Value,
		})
	})

	boff := backoff.New(ctx, backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 2 * time.Second,
		MaxRetries: 0, // retry forever
	})
	for boff.Ongoing() {
		// If the PartitionReader is stopping and the ctx was cancelled, we don't want to interrupt the in-flight
		// processing midway. Instead, we let it finish, assuming it'll succeed.
		// If the processing fails while stopping, we log the error and let the backoff stop and bail out.
		// There is an edge-case when the processing gets stuck and doesn't let the stopping process. In such a case,
		// we expect the infrastructure (e.g. k8s) to eventually kill the process.
		consumeCtx := context.WithoutCancel(ctx)
		consumeStart := time.Now()
		err := r.consumer.Consume(consumeCtx, r.partitionID, records)
		r.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
		if err == nil {
			return nil
		}
		level.Error(r.logger).Log(
			"msg", "encountered error while ingesting data from Kafka; should retry",
			"err", err,
			"record_min_offset", minOffset,
			"record_max_offset", maxOffset,
			"num_retries", boff.NumRetries(),
		)
		boff.Wait()
	}
	// Because boff is set to retry forever, the only error here is when the context is cancelled.
	return boff.ErrCause()
}

func (r *PartitionReader) logFetchErrors(fetches kgo.Fetches) {
	mErr := multierror.New()
	fetches.EachError(func(topic string, partition int32, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}

		// kgo advises to "restart" the kafka client if the returned error is a kerr.Error.
		// Recreating the client would cause duplicate metrics registration, so we don't do it for now.
		mErr.Add(fmt.Errorf("topic %q, partition %d: %w", topic, partition, err))
	})
	if len(mErr) == 0 {
		return
	}
	r.metrics.fetchesErrors.Add(float64(len(mErr)))
	level.Error(r.logger).Log("msg", "encountered error while fetching", "err", mErr.Err())
}

func (p *PartitionReader) pollFetches(ctx context.Context) kgo.Fetches {
	defer func(start time.Time) {
		p.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	return p.client.PollFetches(ctx)
}

func (r *PartitionReader) recordFetchesMetrics(fetches kgo.Fetches, delayObserver prometheus.Observer) {
	var (
		now        = time.Now()
		numRecords = 0
	)

	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		delayObserver.Observe(now.Sub(record.Timestamp).Seconds())
	})

	r.metrics.fetchesTotal.Add(float64(len(fetches)))
	r.metrics.recordsPerFetch.Observe(float64(numRecords))
}

func filterOutErrFetches(fetches kgo.Fetches) kgo.Fetches {
	filtered := make(kgo.Fetches, 0, len(fetches))
	for i, fetch := range fetches {
		if !isErrFetch(fetch) {
			filtered = append(filtered, fetches[i])
		}
	}

	return filtered
}

func isErrFetch(fetch kgo.Fetch) bool {
	for _, t := range fetch.Topics {
		for _, p := range t.Partitions {
			if p.Err != nil {
				return true
			}
		}
	}
	return false
}

type readerMetrics struct {
	receiveDelayWhenStarting prometheus.Observer
	receiveDelayWhenRunning  prometheus.Observer
	recordsPerFetch          prometheus.Histogram
	fetchesErrors            prometheus.Counter
	fetchesTotal             prometheus.Counter
	fetchWaitDuration        prometheus.Histogram
	// strongConsistencyInstrumentation *StrongReadConsistencyInstrumentation[struct{}]
	// lastConsumedOffset               prometheus.Gauge
	consumeLatency prometheus.Histogram
	kprom          *kprom.Metrics
}

func newReaderMetrics(reg prometheus.Registerer) readerMetrics {
	receiveDelay := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:                            "loki_ingest_storage_reader_receive_delay_seconds",
		Help:                            "Delay between producing a record and receiving it in the consumer.",
		NativeHistogramZeroThreshold:    math.Pow(2, -10), // Values below this will be considered to be 0. Equals to 0.0009765625, or about 1ms.
		NativeHistogramBucketFactor:     1.2,              // We use higher factor (scheme=2) to have wider spread of buckets.
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
		Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18), // Buckets between 125ms and 9h.
	}, []string{"phase"})

	return readerMetrics{
		receiveDelayWhenStarting: receiveDelay.WithLabelValues("starting"),
		receiveDelayWhenRunning:  receiveDelay.WithLabelValues("running"),
		kprom:                    kafka.NewReaderClientMetrics("partition-reader", reg),
		fetchWaitDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_wait_duration_seconds",
			Help:                        "How long a consumer spent waiting for a batch of records from the Kafka client. If fetching is faster than processing, then this will be close to 0.",
			NativeHistogramBucketFactor: 1.1,
		}),
		recordsPerFetch: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_storage_reader_records_per_fetch",
			Help:    "The number of records received by the consumer in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		fetchesErrors: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered by the consumer.",
		}),
		fetchesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetches_total",
			Help: "Total number of Kafka fetches received by the consumer.",
		}),
		consumeLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_process_duration_seconds",
			Help:                        "How long a consumer spent processing a batch of records from Kafka.",
			NativeHistogramBucketFactor: 1.1,
		}),
	}
}
