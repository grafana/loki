package distributor

import (
	"context"
	"flag"
	"fmt"
	"time"

	"hash/fnv"
	"sort"
	"strings"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
)

// SegmentTopicConfig configures the SegmentTopicWriter for writing logs to Kafka
// based on configurable segmentation keys derived from stream labels and metadata.
type SegmentTopicConfig struct {
	Enabled bool `yaml:"enabled"`

	// TopicName is the Kafka topic name to write segmented logs to
	TopicName string `yaml:"topic_name"`

	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes flagext.Bytes `yaml:"max_buffered_bytes"`

	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes flagext.Bytes `yaml:"max_record_size_bytes"`

	// NumPartitions is the total number of partitions available for the topic
	NumPartitions int `yaml:"num_partitions"`

	// VolumeAwarePartitioning enables dynamic partition spreading for high-volume segments
	VolumeAwarePartitioning bool `yaml:"volume_aware_partitioning"`

	// VolumeThresholdBytes is the threshold above which segments get spread across multiple partitions
	VolumeThresholdBytes flagext.Bytes `yaml:"volume_threshold_bytes"`

	// MaxPartitionsPerSegment is the maximum number of partitions a single segment can use
	MaxPartitionsPerSegment int `yaml:"max_partitions_per_segment"`

	// VolumeCheckInterval is how often to check stream volumes for volume-aware partitioning
	VolumeCheckInterval time.Duration `yaml:"volume_check_interval"`
}

// RegisterFlags registers the flags for the SegmentTopicConfig
func (cfg *SegmentTopicConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, "distributor.segment-topic.enabled", false, "Enable segment topic writer.")
	fs.StringVar(&cfg.TopicName, "distributor.segment-topic.topic-name", "loki-segments", "Topic name for segment topic writer.")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB default
	fs.Var(&cfg.MaxBufferedBytes, "distributor.segment-topic.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka.")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	fs.Var(&cfg.MaxRecordSizeBytes, "distributor.segment-topic.max-record-size-bytes", "Maximum size of a single Kafka record.")
	fs.IntVar(&cfg.NumPartitions, "distributor.segment-topic.num-partitions", 10, "Number of partitions for the segment topic.")
	fs.BoolVar(&cfg.VolumeAwarePartitioning, "distributor.segment-topic.volume-aware-partitioning", false, "Enable volume-aware partitioning for high-volume segments.")
	cfg.VolumeThresholdBytes = 100 << 20 // 100MB default
	fs.Var(&cfg.VolumeThresholdBytes, "distributor.segment-topic.volume-threshold-bytes", "Volume threshold above which streams get spread across multiple partitions.")
	fs.IntVar(&cfg.MaxPartitionsPerSegment, "distributor.segment-topic.max-partitions-per-segment", 3, "Maximum number of partitions a single segment can use.")
	fs.DurationVar(&cfg.VolumeCheckInterval, "distributor.segment-topic.volume-check-interval", 30*time.Second, "How often to check stream volumes for volume-aware partitioning.")
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

// SegmentTopicWriter implements the Tee interface to write streams to a Kafka topic
// based on configurable segmentation keys. It supports volume-aware partitioning
// to distribute high-volume segments across multiple Kafka partitions.
type SegmentTopicWriter struct {
	cfg      SegmentTopicConfig
	logger   log.Logger
	limits   Limits
	producer *client.Producer

	// Volume-aware partitioning components
	partitionRing ring.PartitionRingReader
	ingestLimits  *ingestLimits

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
	partitionRing ring.PartitionRingReader,
	ingestLimits *ingestLimits,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*SegmentTopicWriter, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("segment topic writer is not enabled")
	}

	t := &SegmentTopicWriter{
		cfg:           cfg,
		logger:        logger,
		limits:        limits,
		partitionRing: partitionRing,
		ingestLimits:  ingestLimits,
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

// write handles the actual writing of streams to the segment topic.
// It updates segment rates for volume tracking and converts streams to Kafka records
// using volume-aware partitioning.
func (s *SegmentTopicWriter) write(tenant string, streams []KeyedStream) {
	// Update segment rates for volume tracking
	s.updateSegmentRates(tenant, streams)

	// Convert streams to Kafka records
	records := make([]*kgo.Record, 0, len(streams))

	for _, stream := range streams {
		// Get partition(s) for this stream (multiple partitions for high-volume segments)
		partitions := s.getPartition(stream, tenant)

		// Create records for each partition
		for _, partition := range partitions {
			streamRecords, err := kafka.EncodeWithTopic(s.cfg.TopicName, partition, tenant, stream.Stream, int(s.cfg.MaxRecordSizeBytes))
			if err != nil {
				s.logger.Log(
					"msg", "failed to encode stream",
					"tenant", tenant,
					"partition", partition,
					"err", err,
				)
				s.teeErrors.WithLabelValues("encode_error").Inc()
				continue
			}

			records = append(records, streamRecords...)
		}
	}

	if len(records) == 0 {
		return
	}

	// Send records to Kafka using the producer
	s.producer.ProduceSync(context.Background(), records)
}

// getPartition determines which partition(s) to use for a given stream.
// It returns a single partition for low-volume segments or multiple partitions
// for high-volume segments when volume-aware partitioning is enabled.
func (s *SegmentTopicWriter) getPartition(stream KeyedStream, tenant string) []int32 {
	segmentationKey := s.getSegmentationKey(stream, tenant)

	// If volume-aware partitioning is disabled, use simple hash-based partitioning
	if !s.cfg.VolumeAwarePartitioning {
		basePartition := s.getBasePartition(segmentationKey)
		return []int32{basePartition}
	}

	// Check if this segment needs volume-based spreading
	needsSpreading := s.needsVolumeSpreading(stream, tenant, segmentationKey)
	if !needsSpreading {
		basePartition := s.getBasePartition(segmentationKey)
		return []int32{basePartition}
	}

	// Use volume-aware partitioning with shuffle sharding
	return s.getVolumeSpreadPartitions(segmentationKey, stream, tenant)
}

// getSegmentationKey creates a segmentation key from the stream labels and metadata
// based on the tenant's configured partitioning keys.
func (s *SegmentTopicWriter) getSegmentationKey(stream KeyedStream, tenant string) string {
	partitioningKeys := s.limits.SegmentTopicPartitionKeys(tenant)

	if len(partitioningKeys) == 0 {
		// Fallback to stream hash
		return fmt.Sprintf("stream_%d", stream.HashKeyNoShard)
	}

	// Use a map to build a unique set of labels for partitioning
	partitioningLabels := make(map[string]string)
	keysToUse := make(map[string]struct{})
	for _, key := range partitioningKeys {
		keysToUse[key] = struct{}{}
	}

	// Extract from stream labels
	parsedLabels, err := syntax.ParseLabels(stream.Stream.Labels)
	if err != nil {
		s.logger.Log("msg", "failed to parse stream labels for partitioning", "tenant", tenant, "labels", stream.Stream.Labels, "err", err)
	} else {
		parsedLabels.Range(func(lbl labels.Label) {
			if _, ok := keysToUse[lbl.Name]; ok {
				partitioningLabels[lbl.Name] = lbl.Value
			}
		})
	}

	// Extract from structured metadata of each entry
	for _, entry := range stream.Stream.Entries {
		for _, sm := range entry.StructuredMetadata {
			if _, ok := keysToUse[sm.Name]; ok {
				partitioningLabels[sm.Name] = sm.Value
			}
		}
	}

	if len(partitioningLabels) == 0 {
		// None of the partitioning keys were found, fallback to stream hash
		return fmt.Sprintf("stream_%d", stream.HashKeyNoShard)
	}

	// Create a stable string representation of the partitioning labels
	keys := make([]string, 0, len(partitioningLabels))
	for k := range partitioningLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(partitioningLabels[k])
	}

	return sb.String()
}

// getBasePartition gets the base partition for a segmentation key
func (s *SegmentTopicWriter) getBasePartition(segmentationKey string) int32 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(segmentationKey))
	hash := hasher.Sum64()
	return int32(hash % uint64(s.cfg.NumPartitions))
}

// needsVolumeSpreading determines if a segment needs volume-based spreading
func (s *SegmentTopicWriter) needsVolumeSpreading(stream KeyedStream, tenant string, segmentationKey string) bool {
	if s.ingestLimits == nil {
		return false
	}

	// Calculate stream size for rate calculation
	entriesSize, structuredMetadataSize := s.calculateStreamSizes(stream.Stream)
	totalSize := int64(entriesSize + structuredMetadataSize)

	// Get current rate from limits service
	rateUpdates := []RateUpdate{{
		Key:  segmentationKey,
		Size: totalSize,
	}}

	results, err := s.ingestLimits.UpdateRates(context.Background(), tenant, rateUpdates)
	if err != nil {
		s.logger.Log("msg", "failed to get segment rate for volume spreading check", "tenant", tenant, "segmentationKey", segmentationKey, "err", err)
		return false
	}

	// Get current rate for this segmentation key
	currentRate, exists := results[segmentationKey]
	if !exists {
		return false
	}

	// Check if the rate exceeds our threshold
	threshold := float64(s.cfg.VolumeThresholdBytes)
	return float64(currentRate) > threshold
}

// getVolumeSpreadPartitions gets multiple partitions for high-volume segments using shuffle sharding
func (s *SegmentTopicWriter) getVolumeSpreadPartitions(segmentationKey string, stream KeyedStream, tenant string) []int32 {
	if s.partitionRing == nil {
		// Fallback to simple hash-based spreading
		return s.getSimpleVolumeSpreadPartitions(segmentationKey, stream)
	}

	// Use shuffle sharding to get a subset of partitions for this segment
	// The shard size is determined by the volume (higher volume = more partitions)
	shardSize := s.calculateShardSize(stream, tenant)

	// Create a subring using shuffle sharding
	subring, err := s.partitionRing.PartitionRing().ShuffleShard(segmentationKey, shardSize)
	if err != nil {
		s.logger.Log("msg", "failed to create shuffle shard", "segmentationKey", segmentationKey, "shardSize", shardSize, "err", err)
		// Fallback to simple hash-based spreading
		return s.getSimpleVolumeSpreadPartitions(segmentationKey, stream)
	}

	// Get all active partitions from the subring
	activePartitions := subring.ActivePartitionIDs()

	// Convert to int32 slice
	partitions := make([]int32, len(activePartitions))
	copy(partitions, activePartitions)

	return partitions
}

// getSimpleVolumeSpreadPartitions provides a fallback when partition ring is not available
func (s *SegmentTopicWriter) getSimpleVolumeSpreadPartitions(segmentationKey string, stream KeyedStream) []int32 {
	// Calculate how many partitions this segment should use based on volume
	shardSize := s.calculateShardSize(stream, "")

	// Use consistent hashing to select partitions
	partitions := make([]int32, 0, shardSize)
	used := make(map[int32]bool)

	// Generate multiple hashes for the same segmentation key to get different partitions
	for i := 0; i < shardSize && len(partitions) < s.cfg.MaxPartitionsPerSegment; i++ {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(fmt.Sprintf("%s_%d", segmentationKey, i)))
		hash := hasher.Sum64()

		partition := int32(hash % uint64(s.cfg.NumPartitions))
		if !used[partition] {
			partitions = append(partitions, partition)
			used[partition] = true
		}
	}

	return partitions
}

// calculateShardSize determines how many partitions a segment should use based on its volume
func (s *SegmentTopicWriter) calculateShardSize(stream KeyedStream, tenant string) int {
	if s.ingestLimits == nil {
		return 1
	}

	segmentationKey := s.getSegmentationKey(stream, tenant)

	// Calculate stream size for rate calculation
	entriesSize, structuredMetadataSize := s.calculateStreamSizes(stream.Stream)
	totalSize := int64(entriesSize + structuredMetadataSize)

	// Get current rate from limits service
	rateUpdates := []RateUpdate{{
		Key:  segmentationKey,
		Size: totalSize,
	}}

	results, err := s.ingestLimits.UpdateRates(context.Background(), tenant, rateUpdates)
	if err != nil {
		s.logger.Log("msg", "failed to get segment rate", "tenant", tenant, "segmentationKey", segmentationKey, "err", err)
		return 1
	}

	// Get current rate for this segmentation key
	currentRate, exists := results[segmentationKey]
	if !exists {
		return 1
	}

	// Calculate shard size based on current rate vs threshold
	threshold := float64(s.cfg.VolumeThresholdBytes)
	ratio := float64(currentRate) / threshold
	shardSize := int(ratio) + 1

	// Cap at MaxPartitionsPerSegment
	if shardSize > s.cfg.MaxPartitionsPerSegment {
		shardSize = s.cfg.MaxPartitionsPerSegment
	}

	// Ensure at least 1 partition
	if shardSize < 1 {
		shardSize = 1
	}

	return shardSize
}

// updateSegmentRates updates segment rates with the limits service
func (s *SegmentTopicWriter) updateSegmentRates(tenant string, streams []KeyedStream) {
	if s.ingestLimits == nil {
		return
	}

	// Group streams by segmentation key and aggregate their sizes
	segmentSizes := make(map[string]int64)

	for _, stream := range streams {
		segmentationKey := s.getSegmentationKey(stream, tenant)
		entriesSize, structuredMetadataSize := s.calculateStreamSizes(stream.Stream)
		totalSize := int64(entriesSize + structuredMetadataSize)
		segmentSizes[segmentationKey] += totalSize
	}

	// Prepare batch update
	rateUpdates := make([]RateUpdate, 0, len(segmentSizes))
	for segmentationKey, totalSize := range segmentSizes {
		rateUpdates = append(rateUpdates, RateUpdate{
			Key:  segmentationKey,
			Size: totalSize,
		})
	}

	// Update segment rates in batch
	_, err := s.ingestLimits.UpdateRates(context.Background(), tenant, rateUpdates)
	if err != nil {
		s.logger.Log("msg", "failed to update segment rates", "tenant", tenant, "err", err)
	}
}

// calculateStreamSizes calculates the total size of a stream's entries and structured metadata
func (s *SegmentTopicWriter) calculateStreamSizes(stream logproto.Stream) (uint64, uint64) {
	var entriesSize, structuredMetadataSize uint64
	for _, entry := range stream.Entries {
		entriesSize += uint64(len(entry.Line))
		structuredMetadataSize += uint64(util.StructuredMetadataSize(entry.StructuredMetadata))
	}
	return entriesSize, structuredMetadataSize
}
