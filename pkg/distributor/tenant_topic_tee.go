package distributor

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// TenantTopicConfig configures the TenantTopicWriter
type TenantTopicConfig struct {
	// TopicPrefix is prepended to tenant IDs to form the final topic name
	TopicPrefix string
	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes int64
	// BatchTimeout is the maximum amount of time to wait before sending a batch
	BatchTimeout time.Duration
	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes int
}

// TenantTopicWriter implements the Tee interface by writing logs to Kafka topics
// based on tenant IDs. Each tenant's logs are written to a dedicated topic.
type TenantTopicWriter struct {
	cfg      TenantTopicConfig
	producer *client.Producer
	logger   log.Logger

	// Metrics
	teeBatchSize    prometheus.Histogram
	teeQueueLatency prometheus.Histogram
	teeErrors       *prometheus.CounterVec
}

// NewTenantTopicWriter creates a new TenantTopicWriter
func NewTenantTopicWriter(
	cfg TenantTopicConfig,
	kafkaClient *kgo.Client,
	reg prometheus.Registerer,
	logger log.Logger,
) (*TenantTopicWriter, error) {
	t := &TenantTopicWriter{
		cfg:    cfg,
		logger: logger,
		producer: client.NewProducer(
			kafkaClient,
			cfg.MaxBufferedBytes,
			reg,
		),
		teeBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "loki_distributor_tenant_topic_tee_batch_size_bytes",
			Help: "Size in bytes of batches sent to Kafka by the tenant topic tee",
			Buckets: []float64{
				1024, 4096, 16384, 65536, 262144, 1048576, 4194304,
			},
		}),
		teeQueueLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_distributor_tenant_topic_tee_queue_duration_seconds",
			Help:    "Duration in seconds spent waiting in queue by the tenant topic tee",
			Buckets: prometheus.DefBuckets,
		}),
		teeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_tenant_topic_tee_errors_total",
			Help: "Total number of errors encountered by the tenant topic tee",
		}, []string{"reason"}),
	}

	if reg != nil {
		reg.MustRegister(t.teeBatchSize, t.teeQueueLatency, t.teeErrors)
	}

	return t, nil
}

// Duplicate implements the Tee interface
func (t *TenantTopicWriter) Duplicate(tenant string, streams []KeyedStream) {
	if len(streams) == 0 {
		return
	}

	// Convert streams to Kafka records
	records := make([]*kgo.Record, 0, len(streams))
	for _, stream := range streams {
		topic := fmt.Sprintf("%s%s", t.cfg.TopicPrefix, tenant)
		// TODO: improve partitioning
		partitionID := int32(stream.HashKey % 32) // Use hash key for consistent partitioning

		streamRecords, err := kafka.EncodeWithTopic(topic, partitionID, tenant, stream.Stream, t.cfg.MaxRecordSizeBytes)
		if err != nil {
			level.Error(t.logger).Log(
				"msg", "failed to encode stream",
				"tenant", tenant,
				"err", err,
			)
			t.teeErrors.WithLabelValues("encode_error").Inc()
			continue
		}

		records = append(records, streamRecords...)
	}

	if len(records) == 0 {
		return
	}

	// Send records to Kafka
	ctx, cancel := context.WithTimeout(context.Background(), t.cfg.BatchTimeout)
	defer cancel()

	results := t.producer.ProduceSync(ctx, records)
	if results.FirstErr() != nil {
		level.Error(t.logger).Log(
			"msg", "failed to write records to kafka",
			"tenant", tenant,
			"err", results.FirstErr(),
		)
		t.teeErrors.WithLabelValues("produce_error").Inc()
		return
	}

	// Update metrics
	var batchSize int64
	for _, record := range records {
		batchSize += int64(len(record.Value))
	}
	t.teeBatchSize.Observe(float64(batchSize))
}

// Close stops the writer and releases resources
func (t *TenantTopicWriter) Close() error {
	t.producer.Close()
	return nil
}
