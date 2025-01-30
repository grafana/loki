package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// Strategy represents the topic creation strategy
type Strategy uint8

const (
	SimpleStrategy Strategy = iota
	AutomaticStrategy
)

func (s Strategy) String() string {
	switch s {
	case SimpleStrategy:
		return "simple"
	case AutomaticStrategy:
		return "automatic"
	default:
		return "unknown"
	}
}

// ParseStrategy converts a string to a Strategy
func ParseStrategy(s string) (Strategy, error) {
	switch s {
	case "simple":
		return SimpleStrategy, nil
	case "automatic":
		return AutomaticStrategy, nil
	default:
		return SimpleStrategy, fmt.Errorf("invalid strategy %q, must be either 'simple' or 'automatic'", s)
	}
}

// codec for `<prefix>.<tenant>.<shard>` topics
type TenantPrefixCodec string

func (c TenantPrefixCodec) Encode(tenant string, shard int32) string {
	return fmt.Sprintf("%s.%s.%d", c, tenant, shard)
}

func (c TenantPrefixCodec) Decode(s string) (tenant string, shard int32, err error) {
	suffix, ok := strings.CutPrefix(s, string(c)+".")
	if !ok {
		return "", 0, fmt.Errorf("invalid format: %s", s)
	}

	rem := strings.Split(suffix, ".")

	if len(rem) < 2 {
		return "", 0, fmt.Errorf("invalid format: %s", s)
	}

	n, err := strconv.Atoi(rem[len(rem)-1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid format: %s", s)
	}

	tenant = strings.Join(rem[:len(rem)-1], ".")

	return tenant, int32(n), nil
}

// TenantTopicConfig configures the TenantTopicWriter
type TenantTopicConfig struct {
	Enabled bool `yaml:"enabled"`

	// TopicPrefix is prepended to tenant IDs to form the final topic name
	TopicPrefix string `yaml:"topic_prefix"`
	// MaxBufferedBytes is the maximum number of bytes that can be buffered before producing to Kafka
	MaxBufferedBytes flagext.Bytes `yaml:"max_buffered_bytes"`

	// MaxRecordSizeBytes is the maximum size of a single Kafka record
	MaxRecordSizeBytes flagext.Bytes `yaml:"max_record_size_bytes"`

	// Strategy determines how topics are created and partitioned
	Strategy string `yaml:"strategy"`
}

// Validate ensures the config is valid
func (cfg *TenantTopicConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.TopicPrefix == "" {
		return errors.New("distributor.tenant-topic-tee.topic-prefix must be set")
	}

	if _, err := ParseStrategy(cfg.Strategy); err != nil {
		return fmt.Errorf("invalid strategy: %w", err)
	}

	return nil
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *TenantTopicConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "distributor.tenant-topic-tee.enabled", false, "Enable the tenant topic tee, which writes logs to Kafka topics based on tenant IDs instead of using multitenant topics/partitions.")
	f.StringVar(&cfg.TopicPrefix, "distributor.tenant-topic-tee.topic-prefix", "loki.tenant", "Prefix to prepend to tenant IDs to form the final Kafka topic name")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB
	f.Var(&cfg.MaxBufferedBytes, "distributor.tenant-topic-tee.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	f.Var(&cfg.MaxRecordSizeBytes, "distributor.tenant-topic-tee.max-record-size-bytes", "Maximum size of a single Kafka record in bytes")
	f.StringVar(&cfg.Strategy, "distributor.tenant-topic-tee.strategy", "simple", "Topic strategy to use. Valid values are 'simple' or 'automatic'")
}

// PartitionResolver resolves the topic and partition for a given tenant and stream
type PartitionResolver interface {
	// Resolve returns the topic and partition for a given tenant and stream
	Resolve(ctx context.Context, tenant string, totalPartitions uint32, stream KeyedStream) (topic string, partition int32, err error)
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
func (r *SimplePartitionResolver) Resolve(_ context.Context, tenant string, totalPartitions uint32, stream KeyedStream) (string, int32, error) {
	topic := fmt.Sprintf("%s.%s", r.topicPrefix, tenant)
	partition := int32(stream.HashKey % totalPartitions)
	return topic, partition, nil
}

// ShardedPartitionResolver implements a partition resolution strategy that creates
// single-partition topics for each tenant and shard combination. This approach offers
// several advantages over traditional multi-partition topics:
//
// 1. Dynamic Scaling:
//   - Topics are created in the form "<prefix>.<tenant>.<shard>"
//   - Each topic has exactly one partition, with shards serving the same purpose
//     as traditional partitions
//   - Unlike traditional partitions which can only be increased but never decreased,
//     this approach allows for both scaling up and down
//   - When volume decreases, we can stop writing to higher-numbered shards
//   - Old shards will be automatically cleaned up through normal retention policies
//
// 2. Consumer Group Flexibility:
//   - Consumers can subscribe to all tenant data using regex pattern "<prefix>.*"
//   - Individual tenants or tenant subsets can be consumed using more specific patterns
//   - Each topic having a single partition simplifies consumer group rebalancing
//
// 3. Topic Management:
//   - Topics are created on-demand when new tenant/shard combinations are encountered
//   - A thread-safe cache (map[tenant]map[shard]struct{}) tracks existing topics
//   - Read lock is used for lookups, write lock for cache updates
//   - Topic creation is idempotent, handling "already exists" errors gracefully
//
// The resolver maintains an internal cache of known tenant/shard combinations to minimize
// admin API calls. When resolving a partition:
// 1. First checks the cache under a read lock
// 2. If not found, releases read lock and attempts to create the topic
// 3. Updates the cache under a write lock
// 4. Returns the topic name with partition 0 (since each topic has exactly one partition)
type ShardedPartitionResolver struct {
	sync.RWMutex
	admin *kadm.Client
	codec TenantPrefixCodec

	sflight singleflight.Group // for topic creation
	// tenantShards maps tenant IDs to their active shards
	// map[tenant]map[shard]struct{}
	tenantShards map[string]map[int32]struct{}
}

// NewShardedPartitionResolver creates a new ShardedPartitionResolver
func NewShardedPartitionResolver(kafkaClient *kgo.Client, topicPrefix string) *ShardedPartitionResolver {
	return &ShardedPartitionResolver{
		admin:        kadm.NewClient(kafkaClient),
		codec:        TenantPrefixCodec(topicPrefix),
		tenantShards: make(map[string]map[int32]struct{}),
	}
}

// Resolve implements PartitionResolver. It ensures a topic exists for the given tenant
// and shard before returning the topic name and partition. If the topic does not exist,
// it will be created with a single partition.
// NB(owen-d): We always use partition 0 since each topic has 1 partition
func (r *ShardedPartitionResolver) Resolve(ctx context.Context, tenant string, totalPartitions uint32, stream KeyedStream) (string, int32, error) {
	// Use the hash key modulo total partitions as the shard number
	shard := int32(stream.HashKey % totalPartitions)

	// Check cache under read lock
	r.RLock()
	shards, exists := r.tenantShards[tenant]
	if exists {
		_, shardExists := shards[shard]
		if shardExists {
			r.RUnlock()
			return r.codec.Encode(tenant, shard), 0, nil
		}
	}
	r.RUnlock()

	// Create the shard if it doesn't exist
	if err := r.createShard(ctx, tenant, shard); err != nil {
		return "", 0, fmt.Errorf("failed to create shard: %w", err)
	}

	return r.codec.Encode(tenant, shard), 0, nil
}

// createShard creates a new topic for the given tenant and shard.
// It handles the case where the topic already exists.
func (r *ShardedPartitionResolver) createShard(ctx context.Context, tenant string, shard int32) error {
	topic := r.codec.Encode(tenant, shard)
	replicationFactor := 2 // TODO: expose RF

	_, err, _ := r.sflight.Do(topic, func() (interface{}, error) {
		return r.admin.CreateTopic(
			ctx,
			1,
			int16(replicationFactor),
			nil,
			topic,
		)
	})

	// Topic creation errors are returned in the response
	if err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		return fmt.Errorf("failed to create topic %s: %w", topic, err)
	}

	// Update cache under write lock
	r.Lock()
	defer r.Unlock()

	shards, exists := r.tenantShards[tenant]
	if !exists {
		shards = make(map[int32]struct{})
		r.tenantShards[tenant] = shards
	}
	shards[shard] = struct{}{}

	return nil
}

// TenantTopicWriter implements the Tee interface by writing logs to Kafka topics
// based on tenant IDs. Each tenant's logs are written to a dedicated topic.
type TenantTopicWriter struct {
	cfg      TenantTopicConfig
	producer *client.Producer
	logger   log.Logger
	resolver PartitionResolver
	limits   IngestionRateLimits

	// Metrics
	teeBatchSize    prometheus.Histogram
	teeQueueLatency prometheus.Histogram
	teeErrors       *prometheus.CounterVec
}

// NewTenantTopicWriter creates a new TenantTopicWriter
func NewTenantTopicWriter(
	cfg TenantTopicConfig,
	kafkaClient *kgo.Client,
	limits IngestionRateLimits,
	reg prometheus.Registerer,
	logger log.Logger,
) (*TenantTopicWriter, error) {
	strategy, err := ParseStrategy(cfg.Strategy)
	if err != nil {
		return nil, fmt.Errorf("invalid strategy: %w", err)
	}

	var resolver PartitionResolver
	switch strategy {
	case SimpleStrategy:
		resolver = NewSimplePartitionResolver(cfg.TopicPrefix)
	case AutomaticStrategy:
		resolver = NewShardedPartitionResolver(kafkaClient, cfg.TopicPrefix)
	default:
		return nil, fmt.Errorf("unknown strategy %q", strategy.String())
	}

	t := &TenantTopicWriter{
		cfg:    cfg,
		logger: logger,
		limits: limits,
		producer: client.NewProducer(
			kafkaClient,
			int64(cfg.MaxBufferedBytes),
			reg,
		),
		resolver: resolver,
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

func (t *TenantTopicWriter) partitionsForRateLimit(bytesRateLimit float64) uint32 {
	targetThroughputPerPartition := float64(10 << 20) // 10 MB

	target := uint32(math.Round(bytesRateLimit / targetThroughputPerPartition))

	// Calculate partitions, ensuring at least 1
	return max(target, 1)
}

// Duplicate implements the Tee interface
func (t *TenantTopicWriter) Duplicate(tenant string, streams []KeyedStream) {
	if len(streams) == 0 {
		return
	}

	go t.write(tenant, streams)
}

func (t *TenantTopicWriter) write(tenant string, streams []KeyedStream) {
	totalPartitions := t.partitionsForRateLimit(t.limits.IngestionRateBytes(tenant))

	// Convert streams to Kafka records
	records := make([]*kgo.Record, 0, len(streams))
	for _, stream := range streams {
		topic, partition, err := t.resolver.Resolve(context.Background(), tenant, totalPartitions, stream)
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
	results := t.producer.ProduceSync(context.TODO(), records)
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

type IngestionRateLimits interface {
	IngestionRateBytes(userID string) float64
}
