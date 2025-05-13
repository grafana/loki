package limits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// Ring
	RingKey  = "ingest-limits"
	RingName = "ingest-limits"

	// Kafka
	consumerGroup = "ingest-limits"
)

// MetadataTopic returns the metadata topic name for the given topic.
func MetadataTopic(topic string) string {
	return topic + ".metadata"
}

var (
	partitionsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_partitions",
		"The current number of partitions.",
		nil,
		nil,
	)
	tenantStreamsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_streams",
		"The current number of streams per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant", "state"},
		nil,
	)
)

type metrics struct {
	tenantStreamEvictionsTotal *prometheus.CounterVec

	kafkaConsumptionLag prometheus.Histogram
	kafkaReadBytesTotal prometheus.Counter

	tenantIngestedBytesTotal *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		tenantStreamEvictionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_stream_evictions_total",
			Help:      "The total number of streams evicted due to age per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		kafkaConsumptionLag: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_ingest_limits_kafka_consumption_lag_seconds",
			Help:                            "The estimated consumption lag in seconds, measured as the difference between the current time and the timestamp of the record.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18),
		}),
		kafkaReadBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_kafka_read_bytes_total",
			Help:      "Total number of bytes read from Kafka.",
		}),
		tenantIngestedBytesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_tenant_ingested_bytes_total",
			Help:      "Total number of bytes ingested per tenant within the active window. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
	}
}

// IngestLimits is a service that manages stream metadata limits.
type IngestLimits struct {
	services.Service

	cfg    Config
	logger log.Logger
	reader *kgo.Client
	writer *kgo.Client

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher

	// metrics
	metrics *metrics

	// limits
	limits Limits

	// Track stream metadata
	usage *UsageStore
	wal   WAL

	// Track partition assignments
	partitionManager *PartitionManager

	// Used for tests.
	clock quartz.Clock
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) TransferOut(_ context.Context) error {
	return nil
}

// NewIngestLimits creates a new IngestLimits service. It initializes the metadata map and sets up a Kafka client
// The client is configured to consume stream metadata from a dedicated topic with the metadata suffix.
func NewIngestLimits(cfg Config, lims Limits, logger log.Logger, reg prometheus.Registerer) (*IngestLimits, error) {
	var err error
	s := &IngestLimits{
		cfg:              cfg,
		logger:           logger,
		usage:            NewUsageStore(cfg),
		metrics:          newMetrics(reg),
		limits:           lims,
		partitionManager: NewPartitionManager(logger),
		clock:            quartz.NewReal(),
	}

	// Initialize internal metadata metrics
	if err := reg.Register(s); err != nil {
		return nil, fmt.Errorf("failed to register ingest limits internal metadata metrics: %w", err)
	}

	// Initialize lifecycler
	s.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, s, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}

	// Watch the lifecycler
	s.lifecyclerWatcher = services.NewFailureWatcher()
	s.lifecyclerWatcher.WatchService(s.lifecycler)

	// Create a copy of the config to modify the topic
	kCfg := cfg.KafkaConfig
	kCfg.Topic = MetadataTopic(kCfg.Topic)
	kCfg.AutoCreateTopicEnabled = true
	kCfg.AutoCreateTopicDefaultPartitions = cfg.NumPartitions

	s.reader, err = client.NewReaderClient("ingest-limits-reader", kCfg, logger, reg,
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(kCfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(s.clock.Now().Add(-s.cfg.WindowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsRevoked),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	s.writer, err = client.NewWriterClient("ingest-limits-writer", kCfg, 20, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}
	s.wal = NewKafkaWAL(s.writer, kCfg.Topic, uint64(s.cfg.NumPartitions), logger)

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *IngestLimits) Describe(descs chan<- *prometheus.Desc) {
	descs <- partitionsDesc
	descs <- tenantStreamsDesc
}

func (s *IngestLimits) Collect(m chan<- prometheus.Metric) {
	cutoff := s.clock.Now().Add(-s.cfg.WindowSize).UnixNano()
	// active counts the number of active streams (within the window) per tenant.
	active := make(map[string]int)
	// expired counts the number of expired streams (outside the window) per tenant.
	expired := make(map[string]int)
	s.usage.All(func(tenant string, _ int32, stream Stream) {
		if stream.LastSeenAt < cutoff {
			expired[tenant]++
		} else {
			active[tenant]++
		}
	})
	for tenant, numActive := range active {
		m <- prometheus.MustNewConstMetric(
			tenantStreamsDesc,
			prometheus.GaugeValue,
			float64(numActive),
			tenant,
			"active",
		)
	}
	for tenant, numExpired := range expired {
		m <- prometheus.MustNewConstMetric(
			tenantStreamsDesc,
			prometheus.GaugeValue,
			float64(numExpired),
			tenant,
			"expired",
		)
	}
	m <- prometheus.MustNewConstMetric(
		partitionsDesc,
		prometheus.GaugeValue,
		float64(len(s.partitionManager.List())),
	)
}

func (s *IngestLimits) onPartitionsAssigned(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.partitionManager.Assign(ctx, client, partitions)
}

func (s *IngestLimits) onPartitionsRevoked(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.partitionManager.Remove(ctx, client, partitions)

	for _, ids := range partitions {
		s.usage.EvictPartitions(ids)
	}
}

func (s *IngestLimits) CheckReady(ctx context.Context) error {
	if s.State() != services.Running {
		return fmt.Errorf("service is not running: %v", s.State())
	}
	err := s.lifecycler.CheckReady(ctx)
	if err != nil {
		return fmt.Errorf("lifecycler not ready: %w", err)
	}
	return nil
}

// starting implements the Service interface's starting method.
// It is called when the service starts and performs any necessary initialization.
func (s *IngestLimits) starting(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			// if starting() fails for any reason (e.g., context canceled),
			// the lifecycler must be stopped.
			_ = services.StopAndAwaitTerminated(context.Background(), s.lifecycler)
		}
	}()

	// pass new context to lifecycler, so that it doesn't stop automatically when IngestLimits's service context is done
	err = s.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = s.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	return nil
}

// running implements the Service interface's running method.
// It runs the main service loop that consumes stream metadata from Kafka and manages
// the metadata map. The method also starts a goroutine to periodically evict old streams from the metadata map.
func (s *IngestLimits) running(ctx context.Context) error {
	// Start the eviction goroutine
	go s.evictOldStreamsPeriodic(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		// stop
		case err := <-s.lifecyclerWatcher.Chan():
			return fmt.Errorf("lifecycler failed: %w", err)
		}
	}
}

// evictOldStreamsPeriodic runs a periodic job that evicts old streams.
// It runs two evictions per window size.
func (s *IngestLimits) evictOldStreamsPeriodic(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WindowSize / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.usage.Evict()
		}
	}
}

// stopping implements the Service interface's stopping method.
// It performs cleanup when the service is stopping, including closing the Kafka client.
// It returns nil for expected termination cases (context cancellation or client closure)
// and returns the original error for other failure cases.
func (s *IngestLimits) stopping(failureCase error) error {
	if s.reader != nil {
		s.reader.Close()
	}

	if s.wal != nil {
		s.wal.Close()
	}

	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}

	var allErrs util.MultiError
	allErrs.Add(services.StopAndAwaitTerminated(context.Background(), s.lifecycler))
	allErrs.Add(failureCase)

	return allErrs.Err()
}

// GetAssignedPartitions implements the proto.IngestLimitsServer interface.
// It returns the partitions that the tenant is assigned to and the instance still owns.
func (s *IngestLimits) GetAssignedPartitions(_ context.Context, _ *proto.GetAssignedPartitionsRequest) (*proto.GetAssignedPartitionsResponse, error) {
	resp := proto.GetAssignedPartitionsResponse{
		AssignedPartitions: s.partitionManager.List(),
	}
	return &resp, nil
}

// ExceedsLimits implements the proto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	var (
		lastSeenAt = s.clock.Now()
		// Calculate the max active streams per tenant per partition
		maxActiveStreams = uint64(s.limits.MaxGlobalStreamsPerUser(req.Tenant) / s.cfg.NumPartitions)
	)

	streams := req.Streams
	valid := 0
	for _, stream := range streams {
		partition := int32(stream.StreamHash % uint64(s.cfg.NumPartitions))

		// TODO(periklis): Do we need to report this as an error to the frontend?
		if assigned := s.partitionManager.Has(partition); !assigned {
			level.Warn(s.logger).Log("msg", "stream assigned partition not owned by instance", "stream_hash", stream.StreamHash, "partition", partition)
			continue
		}

		streams[valid] = stream
		valid++
	}
	streams = streams[:valid]

	cond := streamLimitExceeded(maxActiveStreams)
	accepted, rejected := s.usage.Update(req.Tenant, streams, lastSeenAt, cond)

	var ingestedBytes uint64
	for _, stream := range accepted {
		ingestedBytes += stream.TotalSize

		err := s.wal.Append(context.WithoutCancel(ctx), req.Tenant, stream)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to append stream metadata to WAL", "error", err)
		}
	}

	s.metrics.tenantIngestedBytesTotal.WithLabelValues(req.Tenant).Add(float64(ingestedBytes))

	results := make([]*proto.ExceedsLimitsResult, 0, len(rejected))
	for _, stream := range rejected {
		results = append(results, &proto.ExceedsLimitsResult{
			StreamHash: stream.StreamHash,
			Reason:     uint32(ReasonExceedsMaxStreams),
		})
	}

	return &proto.ExceedsLimitsResponse{Results: results}, nil
}
