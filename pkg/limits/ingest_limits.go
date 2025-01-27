package limits

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	RingKey  = "ingest-limits"
	RingName = "ingest-limits"
)

type metrics struct {
	tenantCurrentRecordedStreams *prometheus.GaugeVec
	tenantStreamEvictionsTotal   *prometheus.CounterVec
	tenantActiveStreams          *prometheus.GaugeVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		tenantCurrentRecordedStreams: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_recorded_streams",
			Help:      "The current number of recorded streams per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		tenantStreamEvictionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_stream_evictions_total",
			Help:      "The total number of streams evicted due to age per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		tenantActiveStreams: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_active_streams",
			Help:      "The current number of active streams (seen within the window) per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
	}
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
	metadata map[string]map[uint64]int64 // tenant -> streamHash -> lastSeenAt
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
		metadata: make(map[string]map[uint64]int64),
		metrics:  newMetrics(reg),
	}

	// Initialize lifecycler
	s.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, s, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}

	// Watch the lifecycler
	s.lifecyclerWatcher = services.NewFailureWatcher()
	s.lifecyclerWatcher.WatchService(s.lifecycler)

	metrics := kprom.NewMetrics("loki_ingest_limits",
		kprom.Registerer(reg),
		kprom.FetchAndProduceDetail(kprom.Batches, kprom.Records, kprom.CompressedBytes, kprom.UncompressedBytes))

	// Create a copy of the config to modify the topic
	kCfg := cfg.KafkaConfig
	kCfg.Topic = kafka.MetadataTopicFor(kCfg.Topic)

	s.client, err = client.NewReaderClient(kCfg, metrics, logger,
		kgo.ConsumerGroup("ingest-limits"),
		kgo.ConsumeTopics(kCfg.Topic),
		kgo.Balancers(kgo.RoundRobinBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-s.cfg.WindowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Debug(logger).Log("msg", "assigned partitions", "partitions", partitions)
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Debug(logger).Log("msg", "revoked partitions", "partitions", partitions)
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Debug(logger).Log("msg", "lost partitions", "partitions", partitions)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
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
			fetches := s.client.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return nil
			}

			if errs := fetches.Errors(); len(errs) > 0 {
				level.Error(s.logger).Log("msg", "error fetching records", "err", errs)
				continue
			}

			// Process the fetched records
			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()
				metadata, err := kafka.DecodeStreamMetadata(record)
				if err != nil {
					level.Error(s.logger).Log("msg", "error decoding metadata", "err", err)
					continue
				}

				s.updateMetadata(metadata, string(record.Key), record.Timestamp)
			}
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
			for tenant, streams := range s.metadata {
				streamsBefore := len(streams)
				evictedCount := 0
				activeCount := 0
				for hash, lastSeen := range streams {
					if lastSeen < cutoff {
						delete(s.metadata[tenant], hash)
						evictedCount++
					} else {
						activeCount++
					}
				}
				// Update eviction counter if any streams were evicted
				if evictedCount > 0 {
					s.metrics.tenantStreamEvictionsTotal.WithLabelValues(tenant).Add(float64(evictedCount))
				}
				// Clean up empty tenant maps and update gauges
				if len(s.metadata[tenant]) == 0 {
					delete(s.metadata, tenant)
					s.metrics.tenantCurrentRecordedStreams.DeleteLabelValues(tenant)
					s.metrics.tenantActiveStreams.DeleteLabelValues(tenant)
				} else {
					if len(streams) != streamsBefore {
						// Only update recorded streams gauge if the number changed
						s.metrics.tenantCurrentRecordedStreams.WithLabelValues(tenant).Set(float64(len(streams)))
					}
					// Always update active streams as they can change even if total count doesn't
					s.metrics.tenantActiveStreams.WithLabelValues(tenant).Set(float64(activeCount))
				}
			}
			s.mtx.Unlock()
		}
	}
}

// updateMetadata updates the metadata map with the provided StreamMetadata.
// It uses the provided lastSeenAt timestamp as the last seen time.
func (s *IngestLimits) updateMetadata(metadata *logproto.StreamMetadata, tenant string, lastSeenAt time.Time) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// Initialize tenant map if it doesn't exist
	if _, ok := s.metadata[tenant]; !ok {
		s.metadata[tenant] = make(map[uint64]int64)
	}

	// Use the provided lastSeenAt timestamp as the last seen time
	recordTime := lastSeenAt.UnixNano()
	if current, ok := s.metadata[tenant][metadata.StreamHash]; !ok || recordTime > current {
		s.metadata[tenant][metadata.StreamHash] = recordTime

		// Count active streams (within window)
		cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()
		activeCount := 0
		for _, lastSeen := range s.metadata[tenant] {
			if lastSeen >= cutoff {
				activeCount++
			}
		}

		// Update gauges
		s.metrics.tenantCurrentRecordedStreams.WithLabelValues(tenant).Set(float64(len(s.metadata[tenant])))
		s.metrics.tenantActiveStreams.WithLabelValues(tenant).Set(float64(activeCount))
	}
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
func (s *IngestLimits) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Calculate stream counts and status per tenant
	type tenantLimits struct {
		Tenant          string   `json:"tenant"`
		ActiveStreams   uint64   `json:"activeStreams"`
		RecordedStreams []uint64 `json:"recordedStreams"`
	}

	response := make(map[string]tenantLimits)
	for tenant, streams := range s.metadata {
		var activeStreams uint64
		recordedStreams := make([]uint64, 0, len(streams))

		// Count active streams and record their status
		for hash, lastSeen := range streams {
			isActive := lastSeen >= cutoff
			if isActive {
				activeStreams++
			}
			recordedStreams = append(recordedStreams, hash)
		}

		if activeStreams > 0 || len(recordedStreams) > 0 {
			response[tenant] = tenantLimits{
				Tenant:          tenant,
				ActiveStreams:   activeStreams,
				RecordedStreams: recordedStreams,
			}
		}
	}

	util.WriteJSONResponse(w, response)
}

// GetStreamUsage implements the logproto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) GetStreamUsage(_ context.Context, req *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Get the tenant's streams
	streams := s.metadata[req.Tenant]
	if streams == nil {
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
		activeStreams   uint64
		recordedStreams = make([]*logproto.RecordedStreams, 0, len(streams))
	)
	for hash, lastSeen := range streams {
		if lastSeen >= cutoff {
			activeStreams++
			recordedStreams = append(recordedStreams, &logproto.RecordedStreams{
				StreamHash: hash,
			})
		}
	}

	return &logproto.GetStreamUsageResponse{
		Tenant:          req.Tenant,
		ActiveStreams:   activeStreams,
		RecordedStreams: recordedStreams,
	}, nil
}
