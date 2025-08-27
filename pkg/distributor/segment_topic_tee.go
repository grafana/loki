package distributor

import (
	"flag"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// SegmentTopicConfig configures the SegmentTopicWriter
type SegmentTopicConfig struct {
	Enabled bool `yaml:"enabled"`

	// TopicName is the name of the topic to write to
	TopicName string `yaml:"topic_name"`

	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes flagext.Bytes `yaml:"max_buffered_bytes"`

	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes flagext.Bytes `yaml:"max_record_size_bytes"`

	// PartitionStrategy determines how partitions are assigned
	PartitionStrategy string `yaml:"partition_strategy"`
}

// RegisterFlags registers the flags for the SegmentTopicConfig
func (cfg *SegmentTopicConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, "distributor.segment-topic.enabled", false, "Enable segment topic writer.")
	fs.StringVar(&cfg.TopicName, "distributor.segment-topic.topic-name", "loki-segments", "Topic name for segment topic writer.")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB default
	fs.Var(&cfg.MaxBufferedBytes, "distributor.segment-topic.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka.")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	fs.Var(&cfg.MaxRecordSizeBytes, "distributor.segment-topic.max-record-size-bytes", "Maximum size of a single Kafka record.")
	fs.StringVar(&cfg.PartitionStrategy, "distributor.segment-topic.partition-strategy", "hash", "Partition strategy for segment topic writer (hash, round-robin, etc.).")
}

// Validate validates the SegmentTopicConfig
func (cfg *SegmentTopicConfig) Validate() error {
	if cfg.Enabled {
		if cfg.TopicName == "" {
			return fmt.Errorf("topic name cannot be empty when segment topic writer is enabled")
		}
		if cfg.MaxBufferedBytes <= 0 {
			return fmt.Errorf("max buffered bytes must be greater than 0")
		}
		if cfg.MaxRecordSizeBytes <= 0 {
			return fmt.Errorf("max record size bytes must be greater than 0")
		}
	}
	return nil
}

// SegmentTopicWriter implements the Tee interface to write streams to a segment topic
type SegmentTopicWriter struct {
	cfg      SegmentTopicConfig
	logger   log.Logger
	limits   Limits
	producer *client.Producer

	// Metrics
	teeBatchSize    prometheus.Histogram
	teeQueueLatency prometheus.Histogram
	teeErrors       *prometheus.CounterVec
}

// NewSegmentTopicWriter creates a new SegmentTopicWriter
func NewSegmentTopicWriter(
	cfg SegmentTopicConfig,
	kafkaClient *kgo.Client,
	limits Limits,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*SegmentTopicWriter, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("segment topic writer is not enabled")
	}

	t := &SegmentTopicWriter{
		cfg:    cfg,
		logger: logger,
		limits: limits,
		producer: client.NewProducer(
			"distributor_segment_topic_tee",
			kafkaClient,
			int64(cfg.MaxBufferedBytes),
			registerer,
		),
		teeBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_distributor_segment_topic_tee_batch_size_bytes",
			Help:    "Size in bytes of batches sent to Kafka by the segment topic tee",
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 100<<20, 10),
		}),
		teeQueueLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_distributor_segment_topic_tee_queue_duration_seconds",
			Help:    "Duration in seconds spent waiting in queue by the segment topic tee",
			Buckets: prometheus.DefBuckets,
		}),
		teeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_segment_topic_tee_errors_total",
			Help: "Total number of errors encountered by the segment topic tee",
		}, []string{"reason"}),
	}

	if registerer != nil {
		registerer.MustRegister(t.teeBatchSize, t.teeQueueLatency, t.teeErrors)
	}

	return t, nil
}

// Duplicate implements the Tee interface
func (s *SegmentTopicWriter) Duplicate(tenant string, streams []KeyedStream) {
	if len(streams) == 0 {
		return
	}

	go s.write(tenant, streams)
}

// write handles the actual writing of streams to the segment topic
func (s *SegmentTopicWriter) write(tenant string, streams []KeyedStream) {
	// Convert streams to Kafka records
	records := make([]*kgo.Record, 0, len(streams))

	for _, stream := range streams {
		// For now, use a simple hash-based partition strategy
		// This can be enhanced with more sophisticated segmentation logic
		partition := s.getPartition(stream, tenant)

		streamRecords, err := kafka.EncodeWithTopic(s.cfg.TopicName, partition, tenant, stream.Stream, int(s.cfg.MaxRecordSizeBytes))
		if err != nil {
			s.logger.Log(
				"msg", "failed to encode stream",
				"tenant", tenant,
				"err", err,
			)
			s.teeErrors.WithLabelValues("encode_error").Inc()
			continue
		}

		records = append(records, streamRecords...)
	}

	if len(records) == 0 {
		return
	}

	// Send records to Kafka
	// Note: This is a simplified implementation - in practice you'd want to handle
	// batching, retries, and error handling more robustly
	// For now, we'll just log that we're writing to the segment topic
	s.logger.Log(
		"msg", "writing streams to segment topic",
		"tenant", tenant,
		"streams", len(streams),
		"records", len(records),
		"topic", s.cfg.TopicName,
	)

	// TODO: Implement actual Kafka writing logic
	// This would involve using the producer to send the records
	// and handling the results appropriately
}

// getPartition determines which partition to use for a given stream
func (s *SegmentTopicWriter) getPartition(stream KeyedStream, tenant string) int32 {
	// Simple hash-based partitioning for now
	// This can be enhanced with more sophisticated segmentation strategies
	hash := stream.HashKeyNoShard
	return int32(hash % 10) // Assuming 10 partitions for now
}
