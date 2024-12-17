package partition

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"

	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type SpecialOffset int

const (
	KafkaStartOffset SpecialOffset = -2
	KafkaEndOffset   SpecialOffset = -1
)

type Record struct {
	// Context holds the tracing (and potentially other) info, that the record was enriched with on fetch from Kafka.
	Ctx      context.Context
	TenantID string
	Content  []byte
	Offset   int64
}

type Reader interface {
	Topic() string
	Partition() int32
	Poll(ctx context.Context, maxPollRecords int) ([]Record, error)
	// Set the target offset for consumption. reads will begin from here.
	SetOffsetForConsumption(offset int64)
	// SetPhase sets the phase for the reader. This is used to differentiate between different phases of the reader.
	// For example, we can use this to differentiate between the startup phase and the running phase.
	SetPhase(phase string)
}

// ReaderMetrics contains metrics specific to Kafka reading operations
type ReaderMetrics struct {
	recordsPerFetch     prometheus.Histogram
	fetchesErrors       prometheus.Counter
	fetchesTotal        prometheus.Counter
	fetchWaitDuration   prometheus.Histogram
	receiveDelay        *prometheus.HistogramVec
	lastCommittedOffset prometheus.Gauge
	kprom               *kprom.Metrics
}

func NewReaderMetrics(r prometheus.Registerer) *ReaderMetrics {
	return &ReaderMetrics{
		fetchWaitDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_kafka_reader_fetch_wait_duration_seconds",
			Help:                        "How long the reader spent waiting for a batch of records from Kafka.",
			NativeHistogramBucketFactor: 1.1,
		}),
		recordsPerFetch: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_kafka_reader_records_per_fetch",
			Help:    "The number of records received in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		fetchesErrors: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered.",
		}),
		fetchesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_reader_fetches_total",
			Help: "Total number of Kafka fetches performed.",
		}),
		receiveDelay: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_kafka_reader_receive_delay_seconds",
			Help:                            "Delay between producing a record and receiving it.",
			NativeHistogramZeroThreshold:    math.Pow(2, -10),
			NativeHistogramBucketFactor:     1.2,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18),
		}, []string{"phase"}),
		kprom: client.NewReaderClientMetrics("partition-reader", r),
	}
}

// KafkaReader provides low-level access to Kafka partition reading operations
type KafkaReader struct {
	client        *kgo.Client
	topic         string
	partitionID   int32
	consumerGroup string
	metrics       *ReaderMetrics
	phase         string
	logger        log.Logger
}

func NewKafkaReader(
	cfg kafka.Config,
	partitionID int32,
	logger log.Logger,
	metrics *ReaderMetrics,
) (*KafkaReader, error) {
	// Create a new Kafka client for this reader
	c, err := client.NewReaderClient(
		cfg,
		metrics.kprom,
		log.With(logger, "component", "kafka-client"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka client: %w", err)
	}

	return &KafkaReader{
		client:      c,
		topic:       cfg.Topic,
		partitionID: partitionID,
		metrics:     metrics,
		logger:      logger,
	}, nil
}

// Topic returns the topic being read
func (r *KafkaReader) Topic() string {
	return r.topic
}

// Partition returns the partition being read
func (r *KafkaReader) Partition() int32 {
	return r.partitionID
}

// SetPhase sets the phase for the reader. This is used to differentiate between different phases of the reader.
// For example, we can use this to differentiate between the startup phase and the running phase.
func (r *KafkaReader) SetPhase(phase string) {
	r.phase = phase
}

// Poll retrieves the next batch of records from Kafka
// Number of records fetched can be limited by configuring maxPollRecords to a non-zero value.
func (r *KafkaReader) Poll(ctx context.Context, maxPollRecords int) ([]Record, error) {
	start := time.Now()
	fetches := r.client.PollRecords(ctx, maxPollRecords)
	r.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())

	// Record metrics
	r.metrics.fetchesTotal.Add(float64(len(fetches)))
	var numRecords int
	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		r.metrics.receiveDelay.WithLabelValues(r.phase).Observe(time.Since(record.Timestamp).Seconds())
	})
	r.metrics.recordsPerFetch.Observe(float64(numRecords))

	// Handle errors
	var errs multierror.MultiError
	fetches.EachError(func(topic string, partition int32, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		errs.Add(fmt.Errorf("topic %q, partition %d: %w", topic, partition, err))
	})
	if len(errs) > 0 {
		r.metrics.fetchesErrors.Add(float64(len(errs)))
		return nil, fmt.Errorf("fetch errors: %v", errs.Err())
	}

	// Build records slice
	records := make([]Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(rec *kgo.Record) {
		if rec.Partition != r.partitionID {
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

	return records, nil
}

func (r *KafkaReader) SetOffsetForConsumption(offset int64) {
	r.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		r.topic: {r.partitionID: kgo.NewOffset().At(offset)},
	})
}
