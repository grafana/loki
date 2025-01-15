package consumer

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring/consumer"
)

const (
	groupName = "dataobj-consumer"
)

type Service struct {
	services.Service

	logger log.Logger
	client *consumer.Client

	builderCfg dataobj.BuilderConfig
	bucket     objstore.Bucket

	// Partition management
	partitionMtx      sync.RWMutex
	partitionHandlers map[string]map[int32]*partitionProcessor
}

func New(kafkaCfg kafka.Config, builderCfg dataobj.BuilderConfig, bucket objstore.Bucket, instanceID string, partitionRing ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *Service {
	s := &Service{
		logger:            log.With(logger, "component", groupName),
		builderCfg:        builderCfg,
		bucket:            bucket,
		partitionHandlers: make(map[string]map[int32]*partitionProcessor),
	}

	client, err := consumer.NewGroupClient(
		kafkaCfg,
		partitionRing,
		groupName,
		client.NewReaderClientMetrics(groupName, reg),
		logger,
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.OnPartitionsAssigned(s.handlePartitionsAssigned),
		kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
			s.handlePartitionsRevoked(m)
		}),
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create consumer", "err", err)
		return nil
	}
	s.client = client
	s.Service = services.NewBasicService(nil, s.run, s.stopping)
	return s
}

func (s *Service) handlePartitionsAssigned(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	level.Info(s.logger).Log("msg", "partitions assigned", "partitions", partitions)
	s.partitionMtx.Lock()
	defer s.partitionMtx.Unlock()

	for topic, parts := range partitions {
		if _, ok := s.partitionHandlers[topic]; !ok {
			s.partitionHandlers[topic] = make(map[int32]*partitionProcessor)
		}

		for _, partition := range parts {
			processor := newPartitionProcessor(ctx, client, s.builderCfg, s.bucket, s.logger, topic, partition)
			s.partitionHandlers[topic][partition] = processor
			processor.start()
		}
	}
}

func (s *Service) handlePartitionsRevoked(partitions map[string][]int32) {
	level.Info(s.logger).Log("msg", "partitions revoked", "partitions", partitions)
	s.partitionMtx.Lock()
	defer s.partitionMtx.Unlock()

	var wg sync.WaitGroup
	for topic, parts := range partitions {
		if handlers, ok := s.partitionHandlers[topic]; ok {
			for _, partition := range parts {
				if processor, exists := handlers[partition]; exists {
					wg.Add(1)
					go func(p *partitionProcessor) {
						defer wg.Done()
						p.stop()
					}(processor)
					delete(handlers, partition)
				}
			}
			if len(handlers) == 0 {
				delete(s.partitionHandlers, topic)
			}
		}
	}
	wg.Wait()
}

func (s *Service) run(ctx context.Context) error {
	for {
		fetches := s.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			level.Error(s.logger).Log("msg", "error fetching records", "err", errs)
			continue
		}
		if fetches.Empty() {
			continue
		}

		// Use a WaitGroup to ensure all records are sent before fetching next batch
		var wg sync.WaitGroup
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			s.partitionMtx.RLock()
			handlers, ok := s.partitionHandlers[ftp.Topic]
			if !ok {
				s.partitionMtx.RUnlock()
				return
			}
			processor, ok := handlers[ftp.Partition]
			s.partitionMtx.RUnlock()
			if !ok {
				return
			}

			// Collect all records for this partition
			records := ftp.Records
			if len(records) == 0 {
				return
			}

			// Launch a goroutine to send records for this partition
			wg.Add(1)
			go func(p *partitionProcessor, recs []*kgo.Record) {
				defer wg.Done()
				for _, record := range recs {
					select {
					case <-p.ctx.Done():
						return
					case p.records <- record:
						// Record sent successfully
					}
				}
			}(processor, records)
		})
		// Wait for all records to be sent before fetching next batch
		wg.Wait()
	}
}

func (s *Service) stopping(failureCase error) error {
	s.partitionMtx.Lock()
	defer s.partitionMtx.Unlock()

	var wg sync.WaitGroup
	for _, handlers := range s.partitionHandlers {
		for _, processor := range handlers {
			wg.Add(1)
			go func(p *partitionProcessor) {
				defer wg.Done()
				p.stop()
			}(processor)
		}
	}
	wg.Wait()
	// Only close the client once all partitions have been stopped.
	// This is to ensure that all records have been processed before closing and offsets committed.
	s.client.Close()
	return failureCase
}
