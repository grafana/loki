package limits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// Ring
	RingKey  = "ingest-limits"
	RingName = "ingest-limits"

	// Kafka
	consumerGroup = "ingest-limits"
)

var (
	tenantPartitionDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_partitions",
		"The current number of partitions per tenant.",
		[]string{"tenant"},
		nil,
	)

	tenantRecordedStreamsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_recorded_streams",
		"The current number of recorded streams per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant"},
		nil,
	)

	tenantActiveStreamsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_active_streams",
		"The current number of active streams (seen within the window) per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant"},
		nil,
	)

	tenantIngestedBytesTotal = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_ingested_bytes_total",
		"The total number of bytes ingested per tenant within the active window. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant"},
		nil,
	)
)

// Config represents the configuration for the ingest limits service.
type Config struct {
	// Enabled enables the ingest limits service.
	Enabled bool `yaml:"enabled"`

	// WindowSize defines the time window for which stream metadata is considered active.
	// Stream metadata older than WindowSize will be evicted from the metadata map.
	WindowSize time.Duration `yaml:"window_size"`

	// RateWindow defines the time window for rate calculation.
	// This should match the window used in Prometheus rate() queries for consistency,
	// when using the `loki_ingest_limits_ingested_bytes_total` metric.
	// Defaults to 5 minutes if not specified.
	RateWindow time.Duration `yaml:"rate_window"`

	// BucketDuration defines the granularity of time buckets used for sliding window rate calculation.
	// Smaller buckets provide more precise rate tracking but require more memory.
	// Defaults to 1 minute if not specified.
	BucketDuration time.Duration `yaml:"bucket_duration"`

	// LifecyclerConfig is the config to build a ring lifecycler.
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	KafkaConfig kafka.Config `yaml:"-"`

	// The number of partitions for the Kafka topic used to read and write stream metadata.
	// It is fixed, not a maximum.
	NumPartitions int `yaml:"num_partitions"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)
	f.BoolVar(&cfg.Enabled, "ingest-limits.enabled", false, "Enable the ingest limits service.")
	f.DurationVar(&cfg.WindowSize, "ingest-limits.window-size", 1*time.Hour, "The time window for which stream metadata is considered active.")
	f.DurationVar(&cfg.RateWindow, "ingest-limits.rate-window", 5*time.Minute, "The time window for rate calculation. This should match the window used in Prometheus rate() queries for consistency.")
	f.DurationVar(&cfg.BucketDuration, "ingest-limits.bucket-duration", 1*time.Minute, "The granularity of time buckets used for sliding window rate calculation. Smaller buckets provide more precise rate tracking but require more memory.")
	f.IntVar(&cfg.NumPartitions, "ingest-limits.num-partitions", 64, "The number of partitions for the Kafka topic used to read and write stream metadata. It is fixed, not a maximum.")
}

func (cfg *Config) Validate() error {
	if cfg.WindowSize <= 0 {
		return errors.New("window-size must be greater than 0")
	}
	if cfg.RateWindow <= 0 {
		return errors.New("rate-window must be greater than 0")
	}
	if cfg.BucketDuration <= 0 {
		return errors.New("bucket-duration must be greater than 0")
	}
	if cfg.RateWindow < cfg.BucketDuration {
		return errors.New("rate-window must be greater than or equal to bucket-duration")
	}
	if cfg.NumPartitions <= 0 {
		return errors.New("num-partitions must be greater than 0")
	}
	return nil
}

type metrics struct {
	tenantStreamEvictionsTotal *prometheus.CounterVec

	kafkaReadLatency    prometheus.Histogram
	kafkaReadBytesTotal prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		tenantStreamEvictionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_stream_evictions_total",
			Help:      "The total number of streams evicted due to age per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		kafkaReadLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "ingest_limits_kafka_read_latency_seconds",
			Help:                            "Latency to read stream metadata from Kafka.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		kafkaReadBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_kafka_read_bytes_total",
			Help:      "Total number of bytes read from Kafka.",
		}),
	}
}

type streamMetadata struct {
	hash       uint64
	lastSeenAt int64
	totalSize  uint64
	// Add a slice to track bytes per time interval for sliding window rate calculation
	rateBuckets []rateBucket
}

// rateBucket represents the bytes received during a specific time interval
type rateBucket struct {
	timestamp int64  // start of the interval
	size      uint64 // bytes received during this interval
}

// IngestLimits is a service that manages stream metadata limits.
type IngestLimits struct {
	services.Service

	cfg    Config
	logger log.Logger
	client *kgo.Client

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher

	// metrics
	metrics *metrics

	// Track stream metadata
	mtx      sync.RWMutex
	metadata map[string]map[int32][]streamMetadata // tenant -> partitionID -> streamMetadata

	// Track partition assignments
	partitionManager *PartitionManager
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) TransferOut(_ context.Context) error {
	return nil
}

// NewIngestLimits creates a new IngestLimits service. It initializes the metadata map and sets up a Kafka client
// The client is configured to consume stream metadata from a dedicated topic with the metadata suffix.
func NewIngestLimits(cfg Config, logger log.Logger, reg prometheus.Registerer) (*IngestLimits, error) {
	var err error
	s := &IngestLimits{
		cfg:              cfg,
		logger:           logger,
		metadata:         make(map[string]map[int32][]streamMetadata),
		metrics:          newMetrics(reg),
		partitionManager: NewPartitionManager(logger),
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

	metrics := client.NewReaderClientMetrics("ingest-limits", reg)

	// Create a copy of the config to modify the topic
	kCfg := cfg.KafkaConfig
	kCfg.Topic = kafka.MetadataTopicFor(kCfg.Topic)
	kCfg.AutoCreateTopicEnabled = true
	kCfg.AutoCreateTopicDefaultPartitions = cfg.NumPartitions

	s.client, err = client.NewReaderClient(kCfg, metrics, logger,
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(kCfg.Topic),
		kgo.Balancers(kgo.StickyBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-s.cfg.WindowSize).UnixMilli())),
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsRevoked),
		kgo.OnPartitionsLost(s.onPartitionsLost),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *IngestLimits) Describe(descs chan<- *prometheus.Desc) {
	descs <- tenantPartitionDesc
	descs <- tenantRecordedStreamsDesc
	descs <- tenantActiveStreamsDesc
	descs <- tenantIngestedBytesTotal
}

func (s *IngestLimits) Collect(m chan<- prometheus.Metric) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	for tenant, partitions := range s.metadata {
		var (
			recorded   int
			active     int
			totalBytes uint64
		)

		for partitionID, partition := range partitions {
			if !s.partitionManager.Has(partitionID) {
				continue
			}

			recorded += len(partition)

			for _, stream := range partition {
				if stream.lastSeenAt >= cutoff {
					active++
					totalBytes += stream.totalSize
				}
			}
		}

		m <- prometheus.MustNewConstMetric(tenantPartitionDesc, prometheus.GaugeValue, float64(len(partitions)), tenant)
		m <- prometheus.MustNewConstMetric(tenantRecordedStreamsDesc, prometheus.GaugeValue, float64(recorded), tenant)
		m <- prometheus.MustNewConstMetric(tenantActiveStreamsDesc, prometheus.GaugeValue, float64(active), tenant)
		m <- prometheus.MustNewConstMetric(tenantIngestedBytesTotal, prometheus.CounterValue, float64(totalBytes), tenant)
	}
}

func (s *IngestLimits) onPartitionsAssigned(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.partitionManager.Assign(ctx, client, partitions)
}

func (s *IngestLimits) onPartitionsRevoked(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.partitionManager.Remove(ctx, client, partitions)
	// TODO(grobinson): Use callbacks from partition manager to delete
	// metadata.
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			// Delete partition from tenant metadata.
			for _, tp := range s.metadata {
				delete(tp, partitionID)
			}
		}
	}
}

func (s *IngestLimits) onPartitionsLost(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.partitionManager.Remove(ctx, client, partitions)
	// TODO(grobinson): Use callbacks from partition manager to delete
	// metadata.
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			// Delete partition from tenant metadata.
			for _, tp := range s.metadata {
				delete(tp, partitionID)
			}
		}
	}
}

func (s *IngestLimits) CheckReady(ctx context.Context) error {
	if s.State() != services.Running && s.State() != services.Stopping {
		return fmt.Errorf("ingest limits not ready: %v", s.State())
	}

	err := s.lifecycler.CheckReady(ctx)
	if err != nil {
		level.Error(s.logger).Log("msg", "ingest limits not ready", "err", err)
		return err
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
		default:
			startTime := time.Now()

			fetches := s.client.PollRecords(ctx, 100)
			if fetches.IsClientClosed() {
				return nil
			}

			if errs := fetches.Errors(); len(errs) > 0 {
				for _, err := range errs {
					level.Error(s.logger).Log("msg", "error fetching records", "err", err.Err.Error())
				}
				continue
			}

			// Record the latency of successful fetches
			s.metrics.kafkaReadLatency.Observe(time.Since(startTime).Seconds())

			// Process the fetched records
			var sizeBytes int

			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()
				sizeBytes += len(record.Value)

				metadata, err := kafka.DecodeStreamMetadata(record)
				if err != nil {
					level.Error(s.logger).Log("msg", "error decoding metadata", "err", err)
					continue
				}

				s.updateMetadata(metadata, string(record.Key), record.Partition, record.Timestamp)
			}

			s.metrics.kafkaReadBytesTotal.Add(float64(sizeBytes))
		}
	}
}

// evictOldStreams evicts old streams. A stream is evicted if it has not
// been seen within the window size.
func (s *IngestLimits) evictOldStreams(_ context.Context) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	for tenant, partitions := range s.metadata {
		evicted := 0
		for partitionID, streams := range partitions {
			// Create a new slice with only active streams
			activeStreams := make([]streamMetadata, 0, len(streams))
			for _, stream := range streams {
				if stream.lastSeenAt >= cutoff {
					activeStreams = append(activeStreams, stream)
				} else {
					evicted++
				}
			}
			s.metadata[tenant][partitionID] = activeStreams

			// If no active streams in this partition, delete it
			if len(activeStreams) == 0 {
				delete(s.metadata[tenant], partitionID)
			}
		}

		// If no partitions left for this tenant, delete the tenant
		if len(s.metadata[tenant]) == 0 {
			delete(s.metadata, tenant)
		}

		s.metrics.tenantStreamEvictionsTotal.WithLabelValues(tenant).Add(float64(evicted))
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
			s.evictOldStreams(ctx)
		}
	}
}

// updateMetadata updates the metadata map with the provided StreamMetadata.
// It uses the provided lastSeenAt timestamp as the last seen time.
func (s *IngestLimits) updateMetadata(rec *logproto.StreamMetadata, tenant string, partition int32, lastSeenAt time.Time) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// Initialize tenant map if it doesn't exist
	if _, ok := s.metadata[tenant]; !ok {
		s.metadata[tenant] = make(map[int32][]streamMetadata)
	}

	if s.metadata[tenant][partition] == nil {
		s.metadata[tenant][partition] = make([]streamMetadata, 0)
	}

	// Partition not assigned to this instance, evict stream
	if assigned := s.partitionManager.Has(partition); !assigned {
		for i, stream := range s.metadata[tenant][partition] {
			if stream.hash == rec.StreamHash {
				s.metadata[tenant][partition] = append(s.metadata[tenant][partition][:i], s.metadata[tenant][partition][i+1:]...)
				break
			}
		}

		return
	}

	// Use the provided lastSeenAt timestamp as the last seen time
	recordTime := lastSeenAt.UnixNano()
	recTotalSize := rec.EntriesSize + rec.StructuredMetadataSize

	// Get the bucket for this timestamp using the configured interval duration
	bucketStart := lastSeenAt.Truncate(s.cfg.BucketDuration).UnixNano()

	// Calculate the rate window cutoff for cleaning up old buckets
	rateWindowCutoff := lastSeenAt.Add(-s.cfg.RateWindow).UnixNano()

	for i, stream := range s.metadata[tenant][partition] {
		if stream.hash == rec.StreamHash {
			// Update total size
			totalSize := stream.totalSize + recTotalSize

			// Update or add size for the current bucket
			updated := false
			sb := make([]rateBucket, 0, len(stream.rateBuckets)+1)

			// Only keep buckets within the rate window and update the current bucket
			for _, bucket := range stream.rateBuckets {
				// Clean up buckets outside the rate window
				if bucket.timestamp < rateWindowCutoff {
					continue
				}

				if bucket.timestamp == bucketStart {
					// Update existing bucket
					sb = append(sb, rateBucket{
						timestamp: bucketStart,
						size:      bucket.size + recTotalSize,
					})
					updated = true
				} else {
					// Keep other buckets within the rate window as is
					sb = append(sb, bucket)
				}
			}

			// Add new bucket if it wasn't updated
			if !updated {
				sb = append(sb, rateBucket{
					timestamp: bucketStart,
					size:      recTotalSize,
				})
			}

			s.metadata[tenant][partition][i] = streamMetadata{
				hash:        stream.hash,
				lastSeenAt:  recordTime,
				totalSize:   totalSize,
				rateBuckets: sb,
			}
			return
		}
	}

	// Create new stream metadata with the initial interval
	s.metadata[tenant][partition] = append(s.metadata[tenant][partition], streamMetadata{
		hash:        rec.StreamHash,
		lastSeenAt:  recordTime,
		totalSize:   recTotalSize,
		rateBuckets: []rateBucket{{timestamp: bucketStart, size: recTotalSize}},
	})
}

// stopping implements the Service interface's stopping method.
// It performs cleanup when the service is stopping, including closing the Kafka client.
// It returns nil for expected termination cases (context cancellation or client closure)
// and returns the original error for other failure cases.
func (s *IngestLimits) stopping(failureCase error) error {
	if s.client != nil {
		s.client.Close()
	}
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}

	var allErrs util.MultiError
	allErrs.Add(services.StopAndAwaitTerminated(context.Background(), s.lifecycler))
	allErrs.Add(failureCase)

	return allErrs.Err()
}

// GetAssignedPartitions implements the logproto.IngestLimitsServer interface.
// It returns the partitions that the tenant is assigned to and the instance still owns.
func (s *IngestLimits) GetAssignedPartitions(_ context.Context, _ *logproto.GetAssignedPartitionsRequest) (*logproto.GetAssignedPartitionsResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	resp := logproto.GetAssignedPartitionsResponse{
		AssignedPartitions: s.partitionManager.List(),
	}
	return &resp, nil
}

// GetStreamUsage implements the logproto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) GetStreamUsage(_ context.Context, req *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Calculate the rate window cutoff in nanoseconds
	rateWindowCutoff := time.Now().Add(-s.cfg.RateWindow).UnixNano()

	// Get the tenant's streams
	partitions := s.metadata[req.Tenant]
	if partitions == nil {
		// If tenant not found, return zero active streams and all requested streams as not recorded
		return &logproto.GetStreamUsageResponse{
			Tenant:        req.Tenant,
			ActiveStreams: 0,
		}, nil
	}

	// Count total active streams for the tenant
	// across all assigned partitions and record
	// the streams that have been seen within the
	// window
	var (
		activeStreams uint64
		totalSize     uint64
	)

	for _, requestedID := range req.Partitions {
		// Consider the recorded stream if it's partition
		// is one of the partitions we are still assigned to.
		assigned := false
		for assignedID := range partitions {
			if requestedID == assignedID {
				assigned = true
				break
			}
		}

		if !assigned {
			continue
		}

		// If the stream is written into a partition we are
		// assigned to and has been seen within the window,
		// it is an active stream.
		for _, stream := range partitions[requestedID] {
			if stream.lastSeenAt < cutoff {
				continue
			}

			activeStreams++

			// Calculate size only within the rate window
			for _, bucket := range stream.rateBuckets {
				if bucket.timestamp >= rateWindowCutoff {
					totalSize += bucket.size
				}
			}
		}
	}

	// Get the unknown streams
	var unknownStreams []uint64
	for _, streamHash := range req.StreamHashes {
		found := false

	outer:
		for _, streams := range partitions {
			for _, stream := range streams {
				if stream.hash == streamHash && stream.lastSeenAt >= cutoff {
					found = true
					break outer
				}
			}
		}

		if !found {
			unknownStreams = append(unknownStreams, streamHash)
			continue
		}
	}

	// Calculate rate using only data from within the rate window
	rate := float64(totalSize) / s.cfg.RateWindow.Seconds()

	// Debug logging to help diagnose rate calculation issues
	level.Debug(s.logger).Log(
		"msg", "calculated stream usage",
		"tenant", req.Tenant,
		"active_streams", activeStreams,
		"total_size", util.HumanizeBytes(totalSize),
		"rate_window_seconds", s.cfg.RateWindow.Seconds(),
		"calculated_rate", util.HumanizeBytes(uint64(rate)),
	)

	return &logproto.GetStreamUsageResponse{
		Tenant:         req.Tenant,
		ActiveStreams:  activeStreams,
		UnknownStreams: unknownStreams,
		Rate:           int64(rate),
	}, nil
}
