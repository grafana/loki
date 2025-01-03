package distributor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	windowSize = 1 * time.Minute
)

// StreamCounter is a service that maintains a count of unique streams per tenant
// by processing records from a Kafka topic.
type StreamCounter struct {
	services.Service

	cfg    kafka.Config
	client *kgo.Client
	counts *streamCounts
}

// streamCounts maintains a thread-safe count of unique streams per tenant.
type streamCounts struct {
	sync.RWMutex
	// tenant -> stream hash -> exists
	counts map[string]map[uint64]time.Time
}

func newStreamCounts() *streamCounts {
	return &streamCounts{
		counts: make(map[string]map[uint64]time.Time),
	}
}

// GetCount returns the number of unique streams for a tenant.
func (s *streamCounts) GetCount(tenant string) int64 {
	s.RLock()
	defer s.RUnlock()

	if streams, ok := s.counts[tenant]; ok {
		return int64(len(streams))
	}
	return 0
}

// AddStream adds a stream hash to a tenant's set of unique streams.
// Returns true if this was a new stream.
func (s *streamCounts) AddStream(tenant string, hash uint64) bool {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.counts[tenant]; !ok {
		s.counts[tenant] = make(map[uint64]time.Time)
	}

	if _, exists := s.counts[tenant][hash]; exists {
		return false
	}

	s.counts[tenant][hash] = time.Now()
	return true
}

// NewStreamCounter creates a new StreamCounter service.
func NewStreamCounter(cfg kafka.Config, consumerGroup string, logger log.Logger, reg prometheus.Registerer) (*StreamCounter, error) {
	counter := &StreamCounter{
		cfg:    cfg,
		counts: newStreamCounts(),
	}

	kprom := client.NewReaderClientMetrics("usage-consumer", reg)
	client, err := client.NewReaderClient(cfg, kprom, logger,
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-windowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Info(logger).Log("msg", "assigned partitions", "partitions", partitions)
		}),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Info(logger).Log("msg", "revoked partitions", "partitions", partitions)
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
			level.Info(logger).Log("msg", "lost partitions", "partitions", partitions)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	counter.Service = services.NewBasicService(counter.starting, counter.running, counter.stopping)
	counter.client = client
	return counter, nil
}

func (s *StreamCounter) starting(ctx context.Context) error {
	return nil
}

func (s *StreamCounter) running(ctx context.Context) error {
	go s.evictOldStreams(ctx)

	decoder, err := kafka.NewDecoder()
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	for {
		fetches := s.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			// Just log the first error and continue
			return fmt.Errorf("error fetching from kafka: %w", errs[0].Err)
		}

		// Process all records in this fetch
		iter := fetches.RecordIter()
		for !iter.Done() {
			record := iter.Next()
			stream, err := decoder.DecodeWithoutLabels(record.Value)
			if err != nil {
				continue
			}

			tenantID := string(record.Key)
			s.counts.AddStream(tenantID, stream.Hash)
		}
	}
}

func (s *StreamCounter) evictOldStreams(ctx context.Context) {
	ticker := time.NewTicker(windowSize / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.counts.Lock()
			defer s.counts.Unlock()

			for tenant, streams := range s.counts.counts {
				for hash, timestamp := range streams {
					if time.Since(timestamp) > windowSize {
						delete(s.counts.counts[tenant], hash)
					}
				}
			}
		}
	}
}

func (s *StreamCounter) stopping(_ error) error {
	if s.client != nil {
		s.client.Close()
	}
	return nil
}

// GetCount returns the number of unique streams for a tenant.
func (s *StreamCounter) GetCount(tenant string) int64 {
	return s.counts.GetCount(tenant)
}
