package limits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	// This should match the window used in Prometheus rate() queries for consistency.
	// Defaults to the same value as WindowSize if not specified.
	RateWindow time.Duration `yaml:"rate_window"`

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
	f.IntVar(&cfg.NumPartitions, "ingest-limits.num-partitions", 64, "The number of partitions for the Kafka topic used to read and write stream metadata. It is fixed, not a maximum.")
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
	assignedPartitions map[int32]int64 // partitionID -> lastAssignedAt
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

	// If RateWindow is not set, default to 5 minutes to match common Prometheus rate() window
	if cfg.RateWindow == 0 {
		cfg.RateWindow = 5 * time.Minute
		level.Info(logger).Log("msg", "RateWindow not set, defaulting to 5m")
	}

	s := &IngestLimits{
		cfg:                cfg,
		logger:             logger,
		metrics:            newMetrics(reg),
		metadata:           make(map[string]map[int32][]streamMetadata),
		assingedPartitions: make(map[int32]int64),
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
			if _, assigned := s.assignedPartitions[partitionID]; !assigned {
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

func (s *IngestLimits) onPartitionsAssigned(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.assignedPartitions == nil {
		s.assignedPartitions = make(map[int32]int64)
	}

	var assigned []string
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			s.assignedPartitions[partitionID] = time.Now().UnixNano()
			assigned = append(assigned, strconv.Itoa(int(partitionID)))
		}
	}

	if len(assigned) > 0 {
		level.Debug(s.logger).Log("msg", "assigned partitions", "partitions", strings.Join(assigned, ","))
	}
}

func (s *IngestLimits) onPartitionsRevoked(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	s.removePartitions(partitions)
}

func (s *IngestLimits) onPartitionsLost(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	s.removePartitions(partitions)
}

func (s *IngestLimits) removePartitions(partitions map[string][]int32) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var dropped int

	for _, partitionIDs := range partitions {
		dropped += len(partitionIDs)
		for _, partitionID := range partitionIDs {
			// Unassign the partition from the ingest limits instance
			delete(s.assignedPartitions, partitionID)

			// Remove the partition from the metadata map
			for tenant, partitions := range s.metadata {
				delete(partitions, partitionID)
				// Check if tenant has any partitions left after deletion
				if len(partitions) == 0 {
					delete(s.metadata, tenant)
				}
			}
		}
	}

	if dropped > 0 {
		level.Debug(s.logger).Log("msg", "removed partitions", "partitions", dropped)
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
	if _, assigned := s.assignedPartitions[partition]; !assigned {
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

	for i, stream := range s.metadata[tenant][partition] {
		if stream.hash == rec.StreamHash {
			s.metadata[tenant][partition][i] = streamMetadata{
				hash:       stream.hash,
				lastSeenAt: recordTime,
				totalSize:  stream.totalSize + recTotalSize,
			}
			return
		}
	}

	s.metadata[tenant][partition] = append(s.metadata[tenant][partition], streamMetadata{
		hash:       rec.StreamHash,
		lastSeenAt: recordTime,
		totalSize:  recTotalSize,
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

// ServeHTTP implements the http.Handler interface.
// It returns the current stream counts and status per tenant as a JSON response.
func (s *IngestLimits) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Get the rate window for rate calculations
	// If RateWindow is configured, use it, otherwise fall back to WindowSize
	var rateWindow time.Duration
	if s.cfg.RateWindow > 0 {
		rateWindow = s.cfg.RateWindow
	} else {
		rateWindow = s.cfg.WindowSize
	}

	// Calculate stream counts and status per tenant
	type tenantLimits struct {
		Tenant        string  `json:"tenant"`
		ActiveStreams uint64  `json:"activeStreams"`
		Rate          float64 `json:"rate"`
	}

	// Get tenant and partitions from query parameters
	params := r.URL.Query()
	tenant := params.Get("tenant")
	var (
		activeStreams uint64
		totalSize     uint64
		response      tenantLimits
	)

	for _, partitions := range s.metadata[tenant] {
		for _, stream := range partitions {
			if stream.lastSeenAt >= cutoff {
				activeStreams++
				totalSize += stream.totalSize
			}
		}
	}

	// Calculate rate using the configured rate window
	// This provides better alignment with Prometheus rate() calculations
	calculatedRate := float64(totalSize) / rateWindow.Seconds()

	if activeStreams > 0 {
		response = tenantLimits{
			Tenant:        tenant,
			ActiveStreams: activeStreams,
			Rate:          calculatedRate,
		}
	} else {
		response = tenantLimits{
			Tenant: tenant,
		}
	}

	// Log the calculated values for debugging
	level.Debug(s.logger).Log(
		"msg", "HTTP endpoint calculated stream usage",
		"tenant", tenant,
		"active_streams", activeStreams,
		"total_size", totalSize,
		"rate_window_seconds", rateWindow.Seconds(),
		"calculated_rate", calculatedRate,
	)

	// Use util.WriteJSONResponse to write the JSON response
	util.WriteJSONResponse(w, response)
}

// GetAssignedPartitions implements the logproto.IngestLimitsServer interface.
// It returns the partitions that the tenant is assigned to and the instance still owns.
func (s *IngestLimits) GetAssignedPartitions(_ context.Context, _ *logproto.GetAssignedPartitionsRequest) (*logproto.GetAssignedPartitionsResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Make a copy of the assigned partitions map to avoid potential concurrent access issues
	partitions := make(map[int32]int64, len(s.assignedPartitions))
	for k, v := range s.assignedPartitions {
		partitions[k] = v
	}

	return &logproto.GetAssignedPartitionsResponse{AssignedPartitions: partitions}, nil
}

// GetStreamUsage implements the logproto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) GetStreamUsage(_ context.Context, req *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Get the rate window cutoff for rate calculations
	// If RateWindow is configured, use it, otherwise fall back to WindowSize
	var rateWindow time.Duration
	if s.cfg.RateWindow > 0 {
		rateWindow = s.cfg.RateWindow
	} else {
		rateWindow = s.cfg.WindowSize
	}

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
			totalSize += stream.totalSize
		}
	}

	// Get the unknown and rate limited streams
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

	// Calculate rate using the configured rate window
	// This provides better alignment with Prometheus rate() calculations
	rate := float64(totalSize) / rateWindow.Seconds()

	// Debug logging to help diagnose rate calculation issues
	level.Debug(s.logger).Log(
		"msg", "calculated stream usage",
		"tenant", req.Tenant,
		"active_streams", activeStreams,
		"total_size", totalSize,
		"rate_window_seconds", rateWindow.Seconds(),
		"calculated_rate", rate,
	)

	return &logproto.GetStreamUsageResponse{
		Tenant:         req.Tenant,
		ActiveStreams:  activeStreams,
		UnknownStreams: unknownStreams,
		Rate:           int64(rate),
	}, nil
}
