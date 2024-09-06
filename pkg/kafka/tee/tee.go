package tee

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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

	ingesterAppends *prometheus.CounterVec
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
		prometheus.WrapRegistererWithPrefix(metricsNamespace+"_kafka_ingester_", registerer))

	t := &Tee{
		logger: log.With(logger, "component", "kafka-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_kafka_ingester_appends_total",
			Help: "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		producer:      producer,
		partitionRing: partitionRing,
		cfg:           cfg,
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

var encoderPool = sync.Pool{
	New: func() any {
		return kafka.NewEncoder()
	},
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
	partitionID, err := t.partitionRing.PartitionRing().ActivePartitionForKey(stream.HashKey)
	if err != nil {
		t.ingesterAppends.WithLabelValues("partition_unknown", "fail").Inc()
		return fmt.Errorf("failed to find active partition for stream: %w", err)
	}

	encoder := encoderPool.Get().(*kafka.Encoder)
	encoder.SetMaxRecordSize(t.cfg.ProducerMaxRecordSizeBytes)
	records, err := encoder.Encode(partitionID, tenant, stream.Stream)
	if err != nil {
		t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
		return fmt.Errorf("failed to marshal write request to records: %w", err)
	}
	encoderPool.Put(encoder)

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenant), writeTimeout)
	defer cancel()
	produceResults := t.producer.ProduceSync(ctx, records)

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
