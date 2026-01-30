package distributor

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
)

type segmentationKey struct {
	str  string
	hash uint64
}

// SegmentTeeConfig configures the SegmentTopicWriter for writing logs to Kafka
// based on configurable segmentation keys derived from stream labels and metadata.
type SegmentTeeConfig struct {
	Enabled bool `yaml:"enabled"`

	// Topic is the Kafka topic name to write segmented logs to
	Topic string `yaml:"topic"`

	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes flagext.Bytes `yaml:"max_buffered_bytes"`

	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes flagext.Bytes `yaml:"max_record_size_bytes"`

	// NumPartitions is the total number of partitions available for the topic
	NumPartitions int `yaml:"num_partitions"`

	// VolumeThresholdBytes is the threshold above which segments get spread across multiple partitions
	VolumeThresholdBytes flagext.Bytes `yaml:"volume_threshold_bytes"`

	// VolumeCheckInterval is how often to check stream volumes for volume-aware partitioning
	VolumeCheckInterval time.Duration `yaml:"volume_check_interval"`

	// TargetThroughputPerPartition is the target throughput per partition in bytes for shuffle sharding
	TargetThroughputPerPartition flagext.Bytes `yaml:"target_throughput_per_partition"`
}

// RegisterFlags registers the flags for the SegmentTopicConfig
func (cfg *SegmentTeeConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cfg.Enabled, "distributor.segment-topic.enabled", false, "Enable segment topic writer.")
	fs.StringVar(&cfg.Topic, "distributor.segment-topic.topic", "loki-segments", "Topic name for segment topic writer.")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB default
	fs.Var(&cfg.MaxBufferedBytes, "distributor.segment-topic.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka.")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	fs.Var(&cfg.MaxRecordSizeBytes, "distributor.segment-topic.max-record-size-bytes", "Maximum size of a single Kafka record.")
	fs.IntVar(&cfg.NumPartitions, "distributor.segment-topic.num-partitions", 10, "Number of partitions for the segment topic.")
	cfg.VolumeThresholdBytes = 100 << 20 // 100MB default
	fs.Var(&cfg.VolumeThresholdBytes, "distributor.segment-topic.volume-threshold-bytes", "Volume threshold above which streams get spread across multiple partitions.")
	fs.DurationVar(&cfg.VolumeCheckInterval, "distributor.segment-topic.volume-check-interval", 30*time.Second, "How often to check stream volumes for volume-aware partitioning.")
	cfg.TargetThroughputPerPartition = 10 << 20 // 10MB default
	fs.Var(&cfg.TargetThroughputPerPartition, "distributor.segment-topic.target-throughput-per-partition", "Target throughput per partition in bytes for shuffle sharding.")
}

// Validate validates the SegmentTopicConfig
func (cfg *SegmentTeeConfig) Validate() error {
	if cfg.Enabled {
		if cfg.Topic == "" {
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
// based on configurable segmentation keys. It always uses volume-aware partitioning
// to distribute high-volume segments across multiple Kafka partitions, and always
// uses shuffle sharding for tenants based on their rate limits.
type SegmentTopicWriter struct {
	cfg      SegmentTeeConfig
	logger   log.Logger
	limits   Limits
	producer *client.Producer

	// Partitioning components
	partitionRing ring.PartitionRingReader
	ingestLimits  *ingestLimits

	// Random number generator for partition selection
	rand *rand.Rand

	// Metrics
	teeBatchSize    prometheus.Histogram
	teeQueueLatency prometheus.Histogram
	teeErrors       *prometheus.CounterVec
}

// NewSegmentTopicWriter creates a new SegmentTopicWriter
func NewSegmentTopicWriter(
	cfg SegmentTeeConfig,
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

	if partitionRing == nil {
		return nil, fmt.Errorf("partition ring is required for segment topic writer")
	}

	t := &SegmentTopicWriter{
		cfg:           cfg,
		logger:        logger,
		limits:        limits,
		partitionRing: partitionRing,
		ingestLimits:  ingestLimits,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
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

type streamWithKey struct {
	stream KeyedStream
	sKey   segmentationKey
}

// write handles the actual writing of streams to the segment topic.
// It updates segment rates for volume tracking and converts streams to Kafka records
// using volume-aware partitioning.
func (s *SegmentTopicWriter) write(tenant string, streams []KeyedStream) {
	streamsWithKeys := make([]streamWithKey, 0, len(streams))
	for _, stream := range streams {
		streamsWithKeys = append(streamsWithKeys, streamWithKey{
			stream: stream,
			sKey:   s.getSegmentationKey(stream, tenant),
		})
	}

	// Update segment rates for volume tracking and get the results
	segmentRates := s.updateSegmentRates(context.Background(), tenant, streamsWithKeys)

	// Convert streams to Kafka records
	records := make([]*kgo.Record, 0, len(streams))

	for _, swk := range streamsWithKeys {
		// Get the rate for this segmentation key
		segmentRate := float64(segmentRates[swk.sKey.hash])

		// TODO(EWELCH): if someone sends a huge push payload this will not be split into multiple
		// partitions, so either we need a limit on the size of the payload or we have some risk
		// here of overwhelming a partition. It may also be possible to split up the push request
		// although that has challenges as well.
		availablePartitions, err := s.getPartition(tenant, swk.sKey, segmentRate)
		if err != nil {
			level.Error(s.logger).Log(
				"msg", "failed to get partitions for stream",
				"tenant", tenant,
				"segmentationKey", swk.sKey.str,
				"err", err,
			)
			s.teeErrors.WithLabelValues("partition_error").Inc()
			continue
		}

		// Select a single partition from the available ones to spread data across partitions
		selectedPartition := s.selectPartition(availablePartitions)

		level.Info(s.logger).Log(
			"msg", "writing to partition",
			"tenant", tenant,
			"stream", swk.stream.Stream.Labels,
			"seg_key_string", swk.sKey.str,
			"seg_key_hash", swk.sKey.hash,
			"available_partitions", fmt.Sprintf("%v", availablePartitions),
			"selected_partition", selectedPartition,
			"seg_rate", segmentRate,
		)

		// Create records for the selected partition only
		streamRecords, err := kafka.EncodeWithTopic(s.cfg.Topic, selectedPartition, tenant, swk.stream.Stream, int(s.cfg.MaxRecordSizeBytes))
		if err != nil {
			level.Error(s.logger).Log(
				"msg", "failed to encode stream",
				"tenant", tenant,
				"partition", selectedPartition,
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

	// Send records to Kafka using the producer
	s.producer.ProduceSync(context.Background(), records)
}

// selectPartition selects a single partition from the available partitions
// using random selection to spread data across partitions.
func (s *SegmentTopicWriter) selectPartition(availablePartitions []int32) int32 {
	if len(availablePartitions) == 0 {
		// Fallback to partition 0 if no partitions available
		return 0
	}
	if len(availablePartitions) == 1 {
		return availablePartitions[0]
	}

	// Randomly select one partition from the available ones
	return availablePartitions[s.rand.Intn(len(availablePartitions))]
}

// getPartition determines which partition(s) to use for a given stream.
// It always uses volume-aware partitioning and shuffle sharding for tenants.
// Low-volume segments get a single partition, high-volume segments get multiple partitions.
func (s *SegmentTopicWriter) getPartition(tenant string, sKey segmentationKey, segmentRate float64) ([]int32, error) {
	// Create tenant subring once based on rate limits for shuffle sharding
	tenantSubring := s.getTenantSubring(tenant)

	// Use volume-aware partitioning with shuffle sharding
	return s.getVolumeSpreadPartitions(sKey, tenant, tenantSubring, segmentRate)
}

// getSegmentationKey creates a segmentation key from the stream labels and metadata
// based on the tenant's configured partitioning rules. It supports both simple keys
// and key=value based rules with additional keys.
func (s *SegmentTopicWriter) getSegmentationKey(stream KeyedStream, tenant string) segmentationKey {
	// Get segmentation rules for this tenant
	var keyStr string
	partitioningRules := s.limits.SegmentationRules(tenant)
	if len(partitioningRules) > 0 {
		segmentationKey, ok := s.getSegmentationKeyFromRules(stream, tenant, partitioningRules)
		if ok {
			keyStr = segmentationKey
		}
	}

	if keyStr == "" {
		// Fallback to stream hash if no rules match
		keyStr = fmt.Sprintf("stream_%d", stream.HashKeyNoShard)
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(keyStr))
	return segmentationKey{str: keyStr, hash: hasher.Sum64()}
}

// getSegmentationKeyFromRules creates a segmentation key using the new flexible rules
func (s *SegmentTopicWriter) getSegmentationKeyFromRules(stream KeyedStream, tenant string, ruleStrings []string) (string, bool) {
	// Parse the segmentation rules
	config, err := ParseSegmentationConfig(ruleStrings)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to parse segmentation rules", "tenant", tenant, "rules", ruleStrings, "err", err)
		return "", false
	}

	// Extract labels and structured metadata
	labels := s.extractLabels(stream)
	structuredMetadata := s.extractStructuredMetadata(stream)

	// Get segmentation keys based on the rules
	segmentationKeys := config.GetSegmentationKeys(labels, structuredMetadata)

	if len(segmentationKeys) == 0 {
		return "", false
	}

	// Create a stable string representation of the segmentation keys
	var sb strings.Builder
	for i, key := range segmentationKeys {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(key)
		sb.WriteString("=")

		// Get the value from labels or structured metadata
		if val, exists := labels[key]; exists {
			sb.WriteString(val)
		} else if val, exists := structuredMetadata[key]; exists {
			sb.WriteString(val)
		} else {
			// This shouldn't happen if GetSegmentationKeys is working correctly
			level.Error(s.logger).Log("msg", "segmentation key found but no value available", "key", key, "tenant", tenant)
			return "", false
		}
	}

	return sb.String(), true
}

// extractLabels extracts labels from the stream
func (s *SegmentTopicWriter) extractLabels(stream KeyedStream) map[string]string {
	labelMap := make(map[string]string)
	parsedLabels, err := syntax.ParseLabels(stream.Stream.Labels)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to parse stream labels", "labels", stream.Stream.Labels, "err", err)
		return labelMap
	}

	parsedLabels.Range(func(lbl labels.Label) {
		labelMap[lbl.Name] = lbl.Value
	})

	return labelMap
}

// extractStructuredMetadata extracts structured metadata from the stream entries
func (s *SegmentTopicWriter) extractStructuredMetadata(stream KeyedStream) map[string]string {
	metadata := make(map[string]string)
	for _, entry := range stream.Stream.Entries {
		for _, sm := range entry.StructuredMetadata {
			metadata[sm.Name] = sm.Value
		}
	}
	return metadata
}

// getBasePartition gets the base partition for a segmentation key
func (s *SegmentTopicWriter) getBasePartition(segmentationKey string) int32 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(segmentationKey))
	hash := hasher.Sum64()
	return int32(hash % uint64(s.cfg.NumPartitions))
}

// getVolumeSpreadPartitions gets multiple partitions for high-volume segments using shuffle sharding.
// It uses the provided tenant subring and creates a further subring for the segment.
// For low-volume segments, it selects a single partition from the tenant's subring.
func (s *SegmentTopicWriter) getVolumeSpreadPartitions(sKey segmentationKey, tenant string, tenantSubring *ring.PartitionRing, segmentRate float64) ([]int32, error) {
	// Calculate how many partitions this segment should use based on its volume
	segmentShardSize := s.calculateShardSize(segmentRate)

	// Get active partitions from the tenant's subring
	tenantPartitions := tenantSubring.ActivePartitionIDs()

	// If no active partitions, fallback to simple hash against total partitions
	if len(tenantPartitions) == 0 {
		return []int32{int32(sKey.hash % uint64(s.cfg.NumPartitions))}, nil
	}

	// Ensure segment shard size doesn't exceed the tenant's available partitions
	if segmentShardSize > len(tenantPartitions) {
		segmentShardSize = len(tenantPartitions)
	}

	// If segment only needs 1 partition, use simple hash-based selection from tenant's subring
	if segmentShardSize <= 1 {
		selectedPartition := tenantPartitions[sKey.hash%uint64(len(tenantPartitions))]
		return []int32{selectedPartition}, nil
	}

	// Create a subring from the tenant's subring for this specific segment
	segmentSubring, err := tenantSubring.ShuffleShard(sKey.str, segmentShardSize)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to create segment shuffle shard", "tenant", tenant, "segmentationKey", sKey.str, "shardSize", segmentShardSize, "err", err)
		// Fallback to using all tenant partitions
		partitions := make([]int32, len(tenantPartitions))
		copy(partitions, tenantPartitions)
		return partitions, nil
	}

	// Get all active partitions from the segment subring
	activePartitions := segmentSubring.ActivePartitionIDs()

	// Convert to int32 slice
	partitions := make([]int32, len(activePartitions))
	copy(partitions, activePartitions)

	return partitions, nil
}

// calculateShardSize determines how many partitions a segment should use based on its volume
func (s *SegmentTopicWriter) calculateShardSize(segmentRate float64) int {
	// Calculate shard size based on current rate vs threshold
	threshold := float64(s.cfg.VolumeThresholdBytes)
	if threshold == 0 {
		return 1
	}

	ratio := segmentRate / threshold
	shardSize := int(math.Ceil(ratio))

	// Ensure at least 1 partition
	if shardSize < 1 {
		shardSize = 1
	}

	return shardSize
}

// updateSegmentRates updates segment rates with the limits service and returns the rate results
func (s *SegmentTopicWriter) updateSegmentRates(ctx context.Context, tenant string, streamsWithKeys []streamWithKey) map[uint64]int64 {
	if s.ingestLimits == nil {
		return make(map[uint64]int64)
	}

	// Group streams by segmentation key and aggregate their sizes
	segmentSizes := make(map[uint64]int64)

	for _, swk := range streamsWithKeys {
		entriesSize, structuredMetadataSize := s.calculateStreamSizes(swk.stream.Stream)
		totalSize := int64(entriesSize + structuredMetadataSize)
		segmentSizes[swk.sKey.hash] += totalSize
	}

	// Prepare batch update
	rateUpdates := make([]*proto.StreamMetadata, 0, len(segmentSizes))
	for segmentationHash, totalSize := range segmentSizes {
		rateUpdates = append(rateUpdates, &proto.StreamMetadata{
			StreamHash: segmentationHash,
			TotalSize:  uint64(totalSize),
		})
	}

	// Update segment rates in batch and return the results
	results, err := s.ingestLimits.UpdateRates(ctx, tenant, rateUpdates)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to update segment rates", "tenant", tenant, "err", err)
		return make(map[uint64]int64)
	}

	return results
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

// tenantShardSize calculates the number of partitions a tenant should use based on their rate limit
// This enables shuffle sharding for small tenants by limiting them to a smaller subset of partitions
func (s *SegmentTopicWriter) tenantShardSize(tenant string) int {
	bytesRateLimit := s.limits.IngestionRateBytes(tenant)
	targetThroughputPerPartition := float64(s.cfg.TargetThroughputPerPartition)

	// Calculate the number of partitions needed based on rate limit
	target := int(math.Round(bytesRateLimit / targetThroughputPerPartition))

	// Ensure at least 1 partition and not more than total partitions
	if target < 1 {
		target = 1
	}
	if target > s.cfg.NumPartitions {
		target = s.cfg.NumPartitions
	}

	return target
}

// getTenantSubring creates a shuffle-sharded subring for the tenant based on their rate limits
func (s *SegmentTopicWriter) getTenantSubring(tenant string) *ring.PartitionRing {
	tenantShardSize := s.tenantShardSize(tenant)

	// If tenant shard size equals total partitions, return the full ring
	if tenantShardSize >= s.cfg.NumPartitions {
		return s.partitionRing.PartitionRing()
	}

	// Create a subring using shuffle sharding with the calculated tenant shard size
	subring, err := s.partitionRing.PartitionRing().ShuffleShard(tenant, tenantShardSize)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to create shuffle shard for tenant", "tenant", tenant, "shardSize", tenantShardSize, "err", err)
		// Fallback to using the full ring
		return s.partitionRing.PartitionRing()
	}

	return subring
}
