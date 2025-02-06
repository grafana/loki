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
)

// Config represents the configuration for the ingest limits service.
type Config struct {
	// Enabled enables the ingest limits service.
	Enabled bool `yaml:"enabled"`
	// WindowSize defines the time window for which stream metadata is considered active.
	// Stream metadata older than WindowSize will be evicted from the metadata map.
	WindowSize time.Duration `yaml:"window_size"`
	// LifecyclerConfig is the config to build a ring lifecycler.
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	// NumPartitions is the number of partitions to use for the ingest limits topic.
	NumPartitions int `yaml:"num_partitions"`

	KafkaConfig kafka.Config `yaml:"-"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits.", f, util_log.Logger)

	f.BoolVar(&cfg.Enabled, "ingest-limits.enabled", false, "Enable the ingest limits service.")
	f.DurationVar(&cfg.WindowSize, "ingest-limits.window-size", 1*time.Hour, "The time window for which stream metadata is considered active.")
	f.IntVar(&cfg.NumPartitions, "ingest-limits.num-partitions", 64, "The number of partitions to use for the ingest limits topic.")
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
	mtxAssingedPartitions sync.RWMutex
	assingedPartitions    map[int32]int64 // partitionID -> lastAssignedAt
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
		cfg:      cfg,
		logger:   logger,
		metadata: make(map[string]map[int32][]streamMetadata),
		metrics:  newMetrics(reg),
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
}

func (s *IngestLimits) Collect(m chan<- prometheus.Metric) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	s.mtxAssingedPartitions.RLock()
	defer s.mtxAssingedPartitions.RUnlock()

	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	for tenant, partitions := range s.metadata {
		var (
			recorded int
			active   int
		)

		for partitionID, partition := range partitions {
			if _, assigned := s.assingedPartitions[partitionID]; !assigned {
				continue
			}

			recorded += len(partition)

			for _, stream := range partition {
				if stream.lastSeenAt >= cutoff {
					active++
				}
			}
		}

		m <- prometheus.MustNewConstMetric(tenantPartitionDesc, prometheus.GaugeValue, float64(len(partitions)), tenant)
		m <- prometheus.MustNewConstMetric(tenantRecordedStreamsDesc, prometheus.GaugeValue, float64(recorded), tenant)
		m <- prometheus.MustNewConstMetric(tenantActiveStreamsDesc, prometheus.GaugeValue, float64(active), tenant)
	}
}

func (s *IngestLimits) onPartitionsAssigned(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	s.mtxAssingedPartitions.Lock()
	defer s.mtxAssingedPartitions.Unlock()

	if s.assingedPartitions == nil {
		s.assingedPartitions = make(map[int32]int64)
	}

	var assigned []string
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			s.assingedPartitions[partitionID] = time.Now().UnixNano()
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
	s.mtxAssingedPartitions.Lock()
	defer s.mtxAssingedPartitions.Unlock()

	s.mtx.Lock()
	defer s.mtx.Unlock()

	var dropped int

	for _, partitionIDs := range partitions {
		dropped += len(partitionIDs)
		for _, partitionID := range partitionIDs {
			// Unassign the partition from the ingest limits instance
			delete(s.assingedPartitions, partitionID)

			// Remove the partition from the metadata map
			for _, partitions := range s.metadata {
				delete(partitions, partitionID)
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
	go s.evictOldStreams(ctx)

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

// evictOldStreams runs as a goroutine and periodically removes streams from the metadata map
// that haven't been seen within the configured window size. It runs every WindowSize/2 interval
// to ensure timely eviction of stale entries.
func (s *IngestLimits) evictOldStreams(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WindowSize / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mtx.Lock()

			cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

			for tenant, partitions := range s.metadata {
				evictedCount := 0

				for partitionID, streams := range partitions {
					for i, stream := range streams {
						if stream.lastSeenAt < cutoff {
							s.metadata[tenant][partitionID] = append(s.metadata[tenant][partitionID][:i], s.metadata[tenant][partitionID][i+1:]...)
							evictedCount++
						}
					}
				}

				// Clean up empty tenant maps and update gauges
				if len(s.metadata[tenant]) == 0 {
					delete(s.metadata, tenant)
				}
				// Only update recorded streams gauge if the number changed
				if evictedCount > 0 {
					s.metrics.tenantStreamEvictionsTotal.WithLabelValues(tenant).Add(float64(evictedCount))
				}
			}
			s.mtx.Unlock()
		}
	}
}

// updateMetadata updates the metadata map with the provided StreamMetadata.
// It uses the provided lastSeenAt timestamp as the last seen time.
func (s *IngestLimits) updateMetadata(rec *logproto.StreamMetadata, tenant string, partition int32, lastSeenAt time.Time) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.mtxAssingedPartitions.RLock()
	defer s.mtxAssingedPartitions.RUnlock()

	// Initialize tenant map if it doesn't exist
	if _, ok := s.metadata[tenant]; !ok {
		s.metadata[tenant] = make(map[int32][]streamMetadata)
	}

	if s.metadata[tenant][partition] == nil {
		s.metadata[tenant][partition] = make([]streamMetadata, 0)
	}

	// Partition not assigned to this instance, evict stream
	if _, assigned := s.assingedPartitions[partition]; !assigned {
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

	for i, stream := range s.metadata[tenant][partition] {
		if stream.hash == rec.StreamHash {
			stream.lastSeenAt = recordTime
			s.metadata[tenant][partition][i] = stream
			return
		}
	}

	s.metadata[tenant][partition] = append(s.metadata[tenant][partition], streamMetadata{
		hash:       rec.StreamHash,
		lastSeenAt: recordTime,
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

	// Calculate stream counts and status per tenant
	type tenantLimits struct {
		Tenant          string   `json:"tenant"`
		ActiveStreams   uint64   `json:"activeStreams"`
		AssignedStreams []uint64 `json:"assignedStreams"`
	}

	// Get tenant and partitions from query parameters
	params := r.URL.Query()
	tenant := params.Get("tenant")
	partitionsStr := params.Get("partitions")

	var requestedPartitions []int32
	if partitionsStr != "" {
		// Split comma-separated partition list
		partitionStrs := strings.Split(partitionsStr, ",")
		requestedPartitions = make([]int32, 0, len(partitionStrs))

		// Convert each partition string to int32
		for _, p := range partitionStrs {
			if val, err := strconv.ParseInt(strings.TrimSpace(p), 10, 32); err == nil {
				requestedPartitions = append(requestedPartitions, int32(val))
			}
		}
	}

	partitions := s.metadata[tenant]

	var (
		activeStreams   uint64
		assignedStreams = make([]uint64, 0)
		response        = make(map[string]tenantLimits)
	)

	for _, requestedID := range requestedPartitions {
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
			if stream.lastSeenAt >= cutoff {
				activeStreams++
				assignedStreams = append(assignedStreams, stream.hash)
			}
		}
	}

	if activeStreams > 0 || len(assignedStreams) > 0 {
		response[tenant] = tenantLimits{
			Tenant:          tenant,
			ActiveStreams:   activeStreams,
			AssignedStreams: assignedStreams,
		}
	}

	util.WriteJSONResponse(w, response)
}

// GetAssignedPartitions implements the logproto.IngestLimitsServer interface.
// It returns the partitions that the tenant is assigned to and the instance still owns.
func (s *IngestLimits) GetAssignedPartitions(_ context.Context, _ *logproto.GetAssignedPartitionsRequest) (*logproto.GetAssignedPartitionsResponse, error) {
	s.mtxAssingedPartitions.RLock()
	defer s.mtxAssingedPartitions.RUnlock()

	return &logproto.GetAssignedPartitionsResponse{AssignedPartitions: s.assingedPartitions}, nil
}

// GetStreamUsage implements the logproto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) GetStreamUsage(_ context.Context, req *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

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
	var activeStreams uint64
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
			if stream.lastSeenAt >= cutoff {
				activeStreams++
			}
		}
	}

	// Get the unknown streams
	var unknownStreams []uint64
	for _, reqHash := range req.StreamHashes {
		found := false

	outer:
		for _, streams := range partitions {
			for _, stream := range streams {
				if stream.hash == reqHash && stream.lastSeenAt >= cutoff {
					found = true
					break outer
				}
			}
		}

		if !found {
			unknownStreams = append(unknownStreams, reqHash)
		}
	}

	return &logproto.GetStreamUsageResponse{
		Tenant:         req.Tenant,
		ActiveStreams:  activeStreams,
		UnknownStreams: unknownStreams,
	}, nil
}
