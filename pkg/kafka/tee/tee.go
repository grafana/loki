package tee

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka"
)

const writeTimeout = time.Minute

// Tee represents a component that duplicates log streams to Kafka.
type Tee struct {
	logger        log.Logger
	producer      *kafka.Producer
	partitionRing ring.PartitionRingReader
	cfg           kafka.Config

	ingesterAppends   *prometheus.CounterVec
	writeLatency      prometheus.Histogram
	writeBytesTotal   prometheus.Counter
	recordsPerRequest prometheus.Histogram
}

// NewTee creates and initializes a new Tee instance.
//
// Parameters:
// - cfg: Kafka configuration
// - metricsNamespace: Namespace for Prometheus metrics
// - registerer: Prometheus registerer for metrics
// - logger: Logger instance
// - partitionRing: Ring for managing partitions
//
// Returns:
// - A new Tee instance and any error encountered during initialization
func NewTee(
	cfg kafka.Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
	partitionRing ring.PartitionRingReader,
) (*Tee, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	kafkaClient, err := kafka.NewWriterClient(cfg, 20, logger, registerer)
	if err != nil {
		return nil, fmt.Errorf("failed to start kafka client: %w", err)
	}
	producer := kafka.NewProducer(kafkaClient, cfg.ProducerMaxBufferedBytes,
		prometheus.WrapRegistererWithPrefix("_kafka_ingester_", registerer))

	t := &Tee{
		logger: log.With(logger, "component", "kafka-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_ingester_appends_total",
			Help: "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		producer:      producer,
		partitionRing: partitionRing,
		cfg:           cfg,
		// Metrics.
		writeLatency: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:                            "kafka_ingester_tee_latency_seconds",
			Help:                            "Latency to write an incoming request to the ingest storage.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		writeBytesTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "kafka_ingester_tee_sent_bytes_total",
			Help: "Total number of bytes sent to the ingest storage.",
		}),
		recordsPerRequest: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "kafka_ingester_tee_records_per_write_request",
			Help:    "The number of records a single per-partition write request has been split into.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 8),
		}),
	}

	return t, nil
}

// Duplicate implements the distributor.Tee interface, which is used to duplicate
// distributor requests to pattern ingesters. It asynchronously sends each stream
// to Kafka.
//
// Parameters:
// - tenant: The tenant identifier
// - streams: A slice of KeyedStream to be duplicated
func (t *Tee) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			if err := t.sendStream(tenant, stream); err != nil {
				level.Error(t.logger).Log("msg", "failed to send stream to kafka", "err", err)
			}
		}(streams[idx])
	}
}

func (t *Tee) Close() {
	t.producer.Close()
}

// sendStream sends a single stream to Kafka.
//
// Parameters:
// - tenant: The tenant identifier
// - stream: The KeyedStream to be sent
//
// Returns:
// - An error if the stream couldn't be sent successfully
func (t *Tee) sendStream(tenant string, stream distributor.KeyedStream) error {
	if len(stream.Stream.Entries) == 0 {
		return nil
	}
	partitionID, err := t.partitionRing.PartitionRing().ActivePartitionForKey(stream.HashKey)
	if err != nil {
		t.ingesterAppends.WithLabelValues("partition_unknown", "fail").Inc()
		return fmt.Errorf("failed to find active partition for stream: %w", err)
	}

	startTime := time.Now()

	records, err := kafka.Encode(partitionID, tenant, stream.Stream, t.cfg.ProducerMaxRecordSizeBytes)
	if err != nil {
		t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
		return fmt.Errorf("failed to marshal write request to records: %w", err)
	}

	t.recordsPerRequest.Observe(float64(len(records)))

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenant), writeTimeout)
	defer cancel()
	produceResults := t.producer.ProduceSync(ctx, records)

	if count, sizeBytes := successfulProduceRecordsStats(produceResults); count > 0 {
		t.writeLatency.Observe(time.Since(startTime).Seconds())
		t.writeBytesTotal.Add(float64(sizeBytes))
	}

	var finalErr error
	for _, result := range produceResults {
		if result.Err != nil {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
			finalErr = err
		} else {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "success").Inc()
		}
	}

	return finalErr
}

func successfulProduceRecordsStats(results kgo.ProduceResults) (count, sizeBytes int) {
	for _, res := range results {
		if res.Err == nil && res.Record != nil {
			count++
			sizeBytes += len(res.Record.Value)
		}
	}

	return
}
