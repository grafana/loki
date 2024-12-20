package usage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

const (
	topic      = "ingest.usage"
	windowSize = 1 * time.Minute
)

type Service struct {
	client *kgo.Client
	services.Service
	decoder *kafka.Decoder
	logger  log.Logger

	stats    *usageStats
	statsMtx sync.RWMutex
}

func NewService(kafkaCfg kafka.Config, consumerGroup string, logger log.Logger, registrar prometheus.Registerer) (*Service, error) {
	kprom := client.NewReaderClientMetrics("usage-consumer", registrar)
	client, err := client.NewReaderClient(kafkaCfg, kprom, logger,
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(topic),
		kgo.Balancers(kgo.RoundRobinBalancer()),
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
		return nil, err
	}
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, err
	}
	s := &Service{
		client:  client,
		logger:  logger,
		decoder: decoder,
		stats:   newUsageStats(),
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *Service) starting(_ context.Context) error {
	return nil
}

func (s *Service) processRecord(partition int32, record *kgo.Record) error {
	tenantID := string(record.Key)
	stream, err := s.decoder.DecodeUsageWithoutLabels(record.Value)
	if err != nil {
		return fmt.Errorf("error decoding usage: %w", err)
	}

	s.statsMtx.Lock()
	defer s.statsMtx.Unlock()

	s.stats.addEntry(
		partition,
		tenantID,
		stream.Hash,
		stream.LineSize+stream.StructuredMetadataSize,
		record.Timestamp,
		record.Offset,
	)

	return nil
}

func (s *Service) evictOldData() {
	s.statsMtx.Lock()
	defer s.statsMtx.Unlock()

	s.stats.evictBefore(time.Now().Add(-windowSize))
}

func (s *Service) runEviction(ctx context.Context) {
	ticker := time.NewTicker(windowSize / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evictOldData()
		}
	}
}

func (s *Service) running(ctx context.Context) error {
	go s.runEviction(ctx)

	for {
		fetches := s.client.PollRecords(ctx, -1)
		for _, fetch := range fetches {
			for _, topicFetch := range fetch.Topics {
				for _, partitionFetch := range topicFetch.Partitions {
					if partitionFetch.Err != nil {
						level.Error(s.logger).Log("msg", "error polling records", "err", partitionFetch.Err)
						return partitionFetch.Err
					}
					for _, record := range partitionFetch.Records {
						if err := s.processRecord(partitionFetch.Partition, record); err != nil {
							level.Error(s.logger).Log("msg", "error processing record", "err", err)
							continue
						}
					}
				}
			}
		}
	}
}

func (s *Service) stopping(failureCase error) error {
	s.client.Close()
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}
	return failureCase
}
