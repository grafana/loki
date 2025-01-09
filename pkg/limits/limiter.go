package limits

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// IngestLimiter is a service that manages stream metadata limits.
type IngestLimiter struct {
	services.Service

	cfg    Config
	client *kgo.Client
	logger log.Logger

	// Track stream metadata
	mtx      sync.RWMutex
	metadata map[uint64]int64 // StreamHash -> LastSeenAt
}

// New creates a new IngestLimiter service. It initializes the metadata map and sets up a Kafka client
// if Kafka is enabled in the configuration. The client is configured to consume stream metadata
// from a dedicated topic with the metadata suffix.
func New(cfg Config, logger log.Logger, reg prometheus.Registerer) (*IngestLimiter, error) {
	var err error
	s := &IngestLimiter{
		cfg:      cfg,
		logger:   logger,
		metadata: make(map[uint64]int64),
	}

	if cfg.KafkaEnabled {
		// Create a copy of the config to modify the topic
		kafkaConfig := cfg.KafkaConfig
		kafkaConfig.Topic = kafka.MetadataTopicFor(kafkaConfig.Topic)

		metrics := kprom.NewMetrics("loki_ingest_limiter", kprom.Registerer(reg))
		s.client, err = client.NewReaderClient(kafkaConfig, metrics, logger,
			kgo.ConsumerGroup("ingest-limiter"),
			kgo.ConsumeTopics(kafkaConfig.Topic),
			kgo.Balancers(kgo.RoundRobinBalancer()),
			kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-cfg.WindowSize).UnixMilli())),
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
			return nil, err
		}
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

// starting implements the Service interface's starting method.
// It is called when the service starts and performs any necessary initialization.
func (s *IngestLimiter) starting(ctx context.Context) error {
	return nil
}

// running implements the Service interface's running method.
// It runs the main service loop that consumes stream metadata from Kafka and manages
// the metadata map. If Kafka is not enabled, it simply waits for context cancellation.
// The method also starts a goroutine to periodically evict old streams from the metadata map.
func (s *IngestLimiter) running(ctx context.Context) error {
	if !s.cfg.KafkaEnabled {
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

				s.updateMetadata(metadata, record.Timestamp)
			}
		}
	}
}

// evictOldStreams runs as a goroutine and periodically removes streams from the metadata map
// that haven't been seen within the configured window size. It runs every WindowSize/2 interval
// to ensure timely eviction of stale entries.
func (s *IngestLimiter) evictOldStreams(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WindowSize / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mtx.Lock()
			cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()
			for hash, lastSeen := range s.metadata {
				if lastSeen < cutoff {
					delete(s.metadata, hash)
				}
			}
			s.mtx.Unlock()
		}
	}
}

// updateMetadata updates the metadata map with the provided StreamMetadata.
// It uses the provided lastSeenAt timestamp as the last seen time for the stream.
func (s *IngestLimiter) updateMetadata(metadata *logproto.StreamMetadata, lastSeenAt time.Time) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// Use the provided lastSeenAt timestamp as the last seen time
	recordTime := lastSeenAt.UnixNano()
	if current, ok := s.metadata[metadata.StreamHash]; !ok || recordTime > current {
		s.metadata[metadata.StreamHash] = recordTime
	}
}

// stopping implements the Service interface's stopping method.
// It performs cleanup when the service is stopping, including closing the Kafka client.
// It returns nil for expected termination cases (context cancellation or client closure)
// and returns the original error for other failure cases.
func (s *IngestLimiter) stopping(failureCase error) error {
	if s.client != nil {
		s.client.Close()
	}
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}
	return failureCase
}
