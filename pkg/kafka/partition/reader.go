package partition

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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

// Reader is responsible for reading data from a specific Kafka partition
// and passing it to the consumer for processing. It is a core component of the
// Loki ingester's Kafka-based ingestion pipeline.
type Reader struct {
	services.Service

	kafkaCfg            kafka.Config
	partitionID         int32
	consumerGroup       string
	consumerFactory     ConsumerFactory
	committer           *partitionCommitter
	lastProcessedOffset int64

	client  *kgo.Client
	logger  log.Logger
	metrics readerMetrics
	reg     prometheus.Registerer
}

type Record struct {
	// Context holds the tracing (and potentially other) info, that the record was enriched with on fetch from Kafka.
	Ctx      context.Context
	TenantID string
	Content  []byte
	Offset   int64
}

type ConsumerFactory func(committer Committer) (Consumer, error)

type Consumer interface {
	Start(ctx context.Context, recordsChan <-chan []Record) func()
}

// NewReader creates and initializes a new PartitionReader.
// It sets up the basic service and initializes the reader with the provided configuration.
func NewReader(
	kafkaCfg kafka.Config,
	partitionID int32,
	instanceID string,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) (*Reader, error) {
	r := &Reader{
		kafkaCfg:            kafkaCfg,
		partitionID:         partitionID,
		consumerGroup:       kafkaCfg.GetConsumerGroup(instanceID, partitionID),
		logger:              logger,
		metrics:             newReaderMetrics(reg),
		reg:                 reg,
		lastProcessedOffset: -1,
		consumerFactory:     consumerFactory,
	}
	r.Service = services.NewBasicService(r.start, r.run, nil)
	return r, nil
}

// start initializes the Kafka client and committer for the PartitionReader.
// This method is called when the PartitionReader service starts.
func (p *Reader) start(_ context.Context) error {
	var err error
	p.client, err = kafka.NewReaderClient(p.kafkaCfg, p.metrics.kprom, p.logger,
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			p.kafkaCfg.Topic: {p.partitionID: kgo.NewOffset().AtStart()},
		}),
	)
	if err != nil {
		return errors.Wrap(err, "creating kafka reader client")
	}
	p.committer = newCommitter(p.kafkaCfg, kadm.NewClient(p.client), p.partitionID, p.consumerGroup, p.logger, p.reg)
	// todo: attempt to ensure max lag timestamp on startup.
	return nil
}

// run is the main loop of the PartitionReader. It continuously fetches and processes
// data from Kafka, and send it to the consumer.
func (p *Reader) run(ctx context.Context) error {
	level.Info(p.logger).Log("msg", "starting partition reader", "partition", p.partitionID, "consumer_group", p.consumerGroup)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	consumer, err := p.consumerFactory(p.committer)
	if err != nil {
		return errors.Wrap(err, "creating consumer")
	}

	recordsChan := p.startFetchLoop(ctx)
	wait := consumer.Start(ctx, recordsChan)

	wait()
	p.committer.Stop()
	return nil
}

func (p *Reader) startFetchLoop(ctx context.Context) <-chan []Record {
	records := make(chan []Record)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				records <- p.poll(ctx)
			}
		}
	}()
	return records
}

// logFetchErrors logs any errors encountered during the fetch operation.
func (p *Reader) logFetchErrors(fetches kgo.Fetches) {
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
func (p *Reader) poll(ctx context.Context) []Record {
	defer func(start time.Time) {
		p.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	fetches := p.client.PollFetches(ctx)
	p.recordFetchesMetrics(fetches)
	p.logFetchErrors(fetches)
	fetches = filterOutErrFetches(fetches)
	if fetches.NumRecords() == 0 {
		return nil
	}
	records := make([]Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(rec *kgo.Record) {
		if rec.Partition != p.partitionID {
			level.Error(p.logger).Log("msg", "wrong partition record received", "partition", rec.Partition, "expected_partition", p.partitionID)
			return
		}
		records = append(records, Record{
			// This context carries the tracing data for this individual record;
			// kotel populates this data when it fetches the messages.
			Ctx:      rec.Context,
			TenantID: string(rec.Key),
			Content:  rec.Value,
			Offset:   rec.Offset,
		})
	})
	p.lastProcessedOffset = records[len(records)-1].Offset
	return records
}

// recordFetchesMetrics updates various metrics related to the fetch operation.
func (p *Reader) recordFetchesMetrics(fetches kgo.Fetches) {
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
	}
}
