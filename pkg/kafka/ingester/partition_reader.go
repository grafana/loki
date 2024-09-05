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

// PartitionReader is responsible for reading data from a specific Kafka partition
// and passing it to the consumer for processing. It is a core component of the
// Loki ingester's Kafka-based ingestion pipeline.
//
// Key features:
// 1. Reads data from a single Kafka partition.
// 2. Periodically flushes processed data and commits offsets.
// 3. Handles backpressure and retries on consumer errors.
//
// Flushing and Committing:
// The PartitionReader implements a periodic flush and commit mechanism:
//   - Every 10 seconds, it calls the consumer's Flush method to ensure all
//     processed data is persisted.
//   - After a successful flush, it commits the last processed offset to Kafka.
//
// This approach ensures that data is regularly persisted and that the ingester
// can resume from the last committed offset in case of a restart or failure.
//
// Note: The flush interval is currently hardcoded but may be made configurable
// in the future to allow fine-tuning based on specific deployment needs.
type PartitionReader struct {
	services.Service

	kafkaCfg            kafka.Config
	partitionID         int32
	consumerGroup       string
	flushInterval       time.Duration
	consumer            Consumer
	committer           *partitionCommitter
	lastProcessedOffset int64

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

// NewPartitionReader creates and initializes a new PartitionReader.
// It sets up the basic service and initializes the reader with the provided configuration.
func NewPartitionReader(
	kafkaCfg kafka.Config,
	partitionID int32,
	instanceID string,
	flushInterval time.Duration,
	consumer Consumer,
	logger log.Logger,
	reg prometheus.Registerer,
) (*PartitionReader, error) {
	r := &PartitionReader{
		kafkaCfg:            kafkaCfg,
		partitionID:         partitionID,
		consumerGroup:       kafkaCfg.GetConsumerGroup(instanceID, partitionID),
		flushInterval:       flushInterval,
		consumer:            consumer,
		logger:              logger,
		metrics:             newReaderMetrics(reg),
		reg:                 reg,
		lastProcessedOffset: -1,
	}
	r.Service = services.NewBasicService(r.start, r.run, nil)
	return r, nil
}

// start initializes the Kafka client and committer for the PartitionReader.
// This method is called when the PartitionReader service starts.
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

// run is the main loop of the PartitionReader. It continuously fetches and processes
// data from Kafka, and periodically flushes and commits offsets.
func (p *PartitionReader) run(ctx context.Context) error {
	level.Info(p.logger).Log("msg", "starting partition reader", "partition", p.partitionID, "consumer_group", p.consumerGroup)

	flushTicker := time.NewTicker(p.flushInterval)
	defer flushTicker.Stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fetchesChan := p.startFetchLoop(ctx)
	return p.loop(ctx, flushTicker.C, fetchesChan)
}

func (p *PartitionReader) loop(ctx context.Context, tickerChan <-chan time.Time, fetchesChan <-chan kgo.Fetches) error {
	for {
		select {
		case <-tickerChan:
			err := p.flushAndCommit(context.Background())
			if err != nil {
				return err
			}
		case <-ctx.Done():
			level.Info(p.logger).Log("msg", "shutting down partition reader", "partition", p.partitionID, "consumer_group", p.consumerGroup)
			err := p.flushAndCommit(context.Background())
			if err != nil {
				return err
			}
			return nil
		case fetches := <-fetchesChan:
			err := p.processNextFetches(ctx, fetches)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					// Fail the whole service in case of a non-recoverable error.
					return err
				}
			}
		}
	}
}

func (p *PartitionReader) startFetchLoop(ctx context.Context) <-chan kgo.Fetches {
	fetchesChan := make(chan kgo.Fetches)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				fetchesChan <- p.pollFetches(ctx)
			}
		}
	}()
	return fetchesChan
}

// flushAndCommit flushes the consumer and commits the last fetched offset.
// It's called periodically and when the PartitionReader is stopping.
func (p *PartitionReader) flushAndCommit(ctx context.Context) error {
	if p.lastProcessedOffset == -1 {
		return nil
	}
	return p.retryWithBackoff(ctx, backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0, // retry forever
	}, func(_ *backoff.Backoff) error {
		err := p.consumer.Flush(ctx)
		if err != nil {
			level.Error(p.logger).Log("msg", "encountered error while flushing", "err", err)
			return err
		}
		if err := p.committer.commit(ctx, p.lastProcessedOffset); err != nil {
			level.Error(p.logger).Log("msg", "encountered error while committing", "err", err)
			return err
		}
		p.lastProcessedOffset = -1
		return nil
	})
}

// getLastFetchOffset retrieves the last offset from the provided fetches.
func (p *PartitionReader) getLastFetchOffset(fetches kgo.Fetches) int64 {
	lastOffset := int64(0)
	fetches.EachPartition(func(partition kgo.FetchTopicPartition) {
		if partition.Partition != p.partitionID {
			level.Error(p.logger).Log("msg", "asked to commit wrong partition", "partition", partition.Partition, "expected_partition", p.partitionID)
			return
		}
		lastOffset = partition.Records[len(partition.Records)-1].Offset
	})
	return lastOffset
}

// processNextFetches fetches the next batch of records from Kafka, processes them,
// and updates metrics. It's the core data processing function of the PartitionReader.
func (p *PartitionReader) processNextFetches(ctx context.Context, fetches kgo.Fetches) error {
	p.recordFetchesMetrics(fetches)
	p.logFetchErrors(fetches)
	fetches = filterOutErrFetches(fetches)

	err := p.consumeFetches(ctx, fetches)
	if err != nil {
		return fmt.Errorf("consume %d records: %w", fetches.NumRecords(), err)
	}
	p.lastProcessedOffset = p.getLastFetchOffset(fetches)
	return nil
}

// consumeFetches processes the fetched records by passing them to the consumer.
// It implements a backoff retry mechanism for handling temporary failures.
func (p *PartitionReader) consumeFetches(ctx context.Context, fetches kgo.Fetches) error {
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
	return p.retryWithBackoff(ctx, backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 2 * time.Second,
		MaxRetries: 0, // retry forever
	}, func(boff *backoff.Backoff) error {
		// If the PartitionReader is stopping and the ctx was cancelled, we don't want to interrupt the in-flight
		// processing midway. Instead, we let it finish, assuming it'll succeed.
		// If the processing fails while stopping, we log the error and let the backoff stop and bail out.
		// There is an edge-case when the processing gets stuck and doesn't let the stopping process. In such a case,
		// we expect the infrastructure (e.g. k8s) to eventually kill the process.
		consumeCtx := context.WithoutCancel(ctx)
		consumeStart := time.Now()
		err := p.consumer.Consume(consumeCtx, p.partitionID, records)
		p.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log(
			"msg", "encountered error while ingesting data from Kafka; should retry",
			"err", err,
			"record_min_offset", minOffset,
			"record_max_offset", maxOffset,
			"num_retries", boff.NumRetries(),
		)
		return err
	})
}

// logFetchErrors logs any errors encountered during the fetch operation.
func (p *PartitionReader) logFetchErrors(fetches kgo.Fetches) {
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
	p.metrics.fetchesErrors.Add(float64(len(mErr)))
	level.Error(p.logger).Log("msg", "encountered error while fetching", "err", mErr.Err())
}

// pollFetches retrieves the next batch of records from Kafka and measures the fetch duration.
func (p *PartitionReader) pollFetches(ctx context.Context) kgo.Fetches {
	defer func(start time.Time) {
		p.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	return p.client.PollFetches(ctx)
}

// recordFetchesMetrics updates various metrics related to the fetch operation.
func (p *PartitionReader) recordFetchesMetrics(fetches kgo.Fetches) {
	var (
		now        = time.Now()
		numRecords = 0
	)

	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		delay := now.Sub(record.Timestamp).Seconds()
		if p.lastProcessedOffset == -1 {
			p.metrics.receiveDelayWhenStarting.Observe(delay)
		} else {
			p.metrics.receiveDelayWhenRunning.Observe(delay)
		}
	})

	p.metrics.fetchesTotal.Add(float64(len(fetches)))
	p.metrics.recordsPerFetch.Observe(float64(numRecords))
}

func (p *PartitionReader) retryWithBackoff(ctx context.Context, cfg backoff.Config, fn func(boff *backoff.Backoff) error) error {
	boff := backoff.New(ctx, cfg)
	var err error
	for boff.Ongoing() {
		err = fn(boff)
		if err == nil {
			return nil
		}
		boff.Wait()
	}
	if err != nil {
		return err
	}
	return boff.ErrCause()
}

// filterOutErrFetches removes any fetches that resulted in errors from the provided slice.
func filterOutErrFetches(fetches kgo.Fetches) kgo.Fetches {
	filtered := make(kgo.Fetches, 0, len(fetches))
	for i, fetch := range fetches {
		if !isErrFetch(fetch) {
			filtered = append(filtered, fetches[i])
		}
	}

	return filtered
}

// isErrFetch checks if a given fetch resulted in any errors.
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

// newReaderMetrics initializes and returns a new set of metrics for the PartitionReader.
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
