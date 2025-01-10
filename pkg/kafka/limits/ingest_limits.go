package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// IngestLimits is a service that manages stream metadata limits.
type IngestLimits struct {
	services.Service

	cfg    kafka.IngestLimitsConfig
	logger log.Logger
	client *kgo.Client

	// Track stream metadata
	mtx      sync.RWMutex
	metadata map[string]map[uint64]int64 // tenant -> streamHash -> lastSeenAt
}

// NewIngestLimits creates a new IngestLimits service. It initializes the metadata map and sets up a Kafka client
// if Kafka is enabled in the configuration. The client is configured to consume stream metadata
// from a dedicated topic with the metadata suffix.
func NewIngestLimits(cfg kafka.Config, logger log.Logger, reg prometheus.Registerer) (*IngestLimits, error) {
	var err error
	s := &IngestLimits{
		cfg:      cfg.IngestLimits,
		logger:   logger,
		metadata: make(map[string]map[uint64]int64),
	}

	if cfg.IngestLimits.Enabled {
		metrics := kprom.NewMetrics("loki_ingest_limits",
			kprom.Registerer(reg),
			kprom.FetchAndProduceDetail(kprom.Batches, kprom.Records, kprom.CompressedBytes, kprom.UncompressedBytes))

		// Create a copy of the config to modify the topic
		kCfg := cfg
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
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

// starting implements the Service interface's starting method.
// It is called when the service starts and performs any necessary initialization.
func (s *IngestLimits) starting(ctx context.Context) error {
	return nil
}

// running implements the Service interface's running method.
// It runs the main service loop that consumes stream metadata from Kafka and manages
// the metadata map. If Kafka is not enabled, it simply waits for context cancellation.
// The method also starts a goroutine to periodically evict old streams from the metadata map.
func (s *IngestLimits) running(ctx context.Context) error {
	if !s.cfg.Enabled {
		<-ctx.Done()
		return nil
	}

	// Start the eviction goroutine
	go s.evictOldStreams(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
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
				for hash, lastSeen := range streams {
					if lastSeen < cutoff {
						delete(s.metadata[tenant], hash)
					}
				}
				// Clean up empty tenant maps
				if len(s.metadata[tenant]) == 0 {
					delete(s.metadata, tenant)
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
	return failureCase
}

// GetLimits implements the IngestLimits service. It returns the current ingestion limits
// for a given tenant, including stream count and stream rates.
func (s *IngestLimits) GetLimits(ctx context.Context, req *logproto.GetLimitsRequest) (*logproto.GetLimitsResponse, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get streams for the requested tenant
	streams, ok := s.metadata[req.Tenant]
	if !ok {
		return &logproto.GetLimitsResponse{
			Limits: &logproto.IngestionLimits{
				StreamCount: 0,
				StreamRates: nil,
			},
		}, nil
	}

	streamCount := uint64(0)
	streamRates := make([]*logproto.StreamRate, 0)

	// Collect all stream rates for the tenant
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()
	for hash, lastSeen := range streams {
		if lastSeen < cutoff {
			continue // Skip expired entries
		}
		streamCount++
		streamRates = append(streamRates, &logproto.StreamRate{
			StreamHash: hash,
			Tenant:     req.Tenant,
		})
	}

	return &logproto.GetLimitsResponse{
		Limits: &logproto.IngestionLimits{
			StreamCount: streamCount,
			StreamRates: streamRates,
		},
	}, nil
}

// ServeHTTP implements the http.Handler interface.
// It returns the current stream counts per tenant as a JSON response.
func (s *IngestLimits) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Calculate stream counts per tenant
	counts := make(map[string]int)
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	for tenant, streams := range s.metadata {
		activeStreams := 0
		for _, lastSeen := range streams {
			if lastSeen >= cutoff {
				activeStreams++
			}
		}
		if activeStreams > 0 {
			counts[tenant] = activeStreams
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(counts); err != nil {
		http.Error(w, fmt.Sprintf("error encoding stream counts: %v", err), http.StatusInternalServerError)
		return
	}
}
