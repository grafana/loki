package distributor

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// PartitionResolver resolves the topic and partition for a given tenant and stream
type PartitionResolver interface {
	// Resolve returns the topic and partition for a given tenant and stream
	Resolve(ctx context.Context, tenant string, stream KeyedStream) (topic string, partition int32, err error)
}

// SimplePartitionResolver is a basic implementation of PartitionResolver that
// uses a prefix for topics and hash-based partitioning
type SimplePartitionResolver struct {
	topicPrefix string
}

// NewSimplePartitionResolver creates a new SimplePartitionResolver
func NewSimplePartitionResolver(topicPrefix string) *SimplePartitionResolver {
	return &SimplePartitionResolver{
		topicPrefix: topicPrefix,
	}
}

// Resolve implements PartitionResolver
func (r *SimplePartitionResolver) Resolve(_ context.Context, tenant string, stream KeyedStream) (string, int32, error) {
	topic := fmt.Sprintf("%s%s", r.topicPrefix, tenant)
	partition := int32(stream.HashKey % 32) // Use hash key for consistent partitioning
	return topic, partition, nil
}

// TenantTopicConfig configures the TenantTopicWriter
type TenantTopicConfig struct {
	Enabled bool

	// TopicPrefix is prepended to tenant IDs to form the final topic name
	TopicPrefix string
	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes flagext.Bytes

	// BatchTimeout is the maximum amount of time to wait before sending a batch
	BatchTimeout time.Duration
	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes flagext.Bytes
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *TenantTopicConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "distributor.tenant-topic-tee.enabled", false, "Enable the tenant topic tee")
	f.StringVar(&cfg.TopicPrefix, "distributor.tenant-topic-tee.topic-prefix", "loki.tenant.", "Prefix to prepend to tenant IDs to form the final Kafka topic name")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB
	f.Var(&cfg.MaxBufferedBytes, "distributor.tenant-topic-tee.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka")
	f.DurationVar(&cfg.BatchTimeout, "distributor.tenant-topic-tee.batch-timeout", time.Second, "Maximum amount of time to wait before sending a batch to Kafka")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	f.Var(&cfg.MaxRecordSizeBytes, "distributor.tenant-topic-tee.max-record-size-bytes", "Maximum size of a single Kafka record in bytes")
}

// TenantTopicWriter implements the Tee interface by writing logs to Kafka topics
// based on tenant IDs. Each tenant's logs are written to a dedicated topic.
type TenantTopicWriter struct {
	cfg      TenantTopicConfig
	producer *client.Producer
	logger   log.Logger
	resolver PartitionResolver

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
			int64(cfg.MaxBufferedBytes),
			reg,
		),
		resolver: NewSimplePartitionResolver(cfg.TopicPrefix),
		teeBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_distributor_tenant_topic_tee_batch_size_bytes",
			Help:    "Size in bytes of batches sent to Kafka by the tenant topic tee",
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 100<<20, 10),
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
		topic, partition, err := t.resolver.Resolve(context.Background(), tenant, stream)
		if err != nil {
			level.Error(t.logger).Log(
				"msg", "failed to resolve topic and partition",
				"tenant", tenant,
				"err", err,
			)
			t.teeErrors.WithLabelValues("resolve_error").Inc()
			continue
		}

		streamRecords, err := kafka.EncodeWithTopic(topic, partition, tenant, stream.Stream, int(t.cfg.MaxRecordSizeBytes))
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
