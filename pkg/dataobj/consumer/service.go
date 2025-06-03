package consumer

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/indexing"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/indexing/indexobj"
	"github.com/grafana/loki/v3/pkg/distributor"
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
	reg    prometheus.Registerer
	client *consumer.Client

	eventsProducerClient *kgo.Client
	eventConsumerClient  *kgo.Client

	cfg    Config
	bucket objstore.Bucket
	codec  distributor.TenantPrefixCodec

	// Partition management
	partitionMtx      sync.RWMutex
	partitionHandlers map[string]map[int32]*partitionProcessor

	bufPool *sync.Pool
}

func New(kafkaCfg kafka.Config, cfg Config, topicPrefix string, bucket objstore.Bucket, instanceID string, partitionRing ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *Service {
	s := &Service{
		logger:            log.With(logger, "component", groupName),
		cfg:               cfg,
		bucket:            bucket,
		codec:             distributor.TenantPrefixCodec(topicPrefix),
		partitionHandlers: make(map[string]map[int32]*partitionProcessor),
		reg:               reg,
		bufPool: &sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, cfg.BuilderConfig.TargetObjectSize))
			},
		},
	}

	if cfg.PartitionProcessingEnabled {
		consumerClient, err := consumer.NewGroupClient(
			kafkaCfg,
			partitionRing,
			groupName,
			logger,
			reg,
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

		eventsKafkaCfg := kafkaCfg
		eventsKafkaCfg.Topic = "loki.metastore-events"
		eventsKafkaCfg.AutoCreateTopicDefaultPartitions = 1
		eventsProducerClient, err := client.NewWriterClient("loki.metastore-events", eventsKafkaCfg, 50, logger, reg)
		if err != nil {
			level.Error(logger).Log("msg", "failed to create producer", "err", err)
			return nil
		}
		s.client = consumerClient
		s.eventsProducerClient = eventsProducerClient
	}

	if cfg.IndexBuildingEnabled {
		consumerCfg := kafkaCfg
		consumerCfg.AutoCreateTopicEnabled = true
		consumerCfg.AutoCreateTopicDefaultPartitions = 1
		eventConsumerClient, err := client.NewReaderClient(
			"eventReader",
			consumerCfg,
			logger,
			reg,
			kgo.ConsumeTopics("loki.metastore-events"),
			kgo.InstanceID(instanceID),
			kgo.SessionTimeout(3*time.Minute),
			kgo.ConsumerGroup("metastore-event-reader"),
			kgo.RebalanceTimeout(5*time.Minute),
			kgo.DisableAutoCommit(),
		)
		if err != nil {
			level.Error(logger).Log("msg", "failed to create consumer", "err", err)
			return nil
		}
		s.eventConsumerClient = eventConsumerClient
	}

	s.Service = services.NewBasicService(nil, s.run, s.stopping)
	return s
}

func (s *Service) handlePartitionsAssigned(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	level.Info(s.logger).Log("msg", "partitions assigned", "partitions", formatPartitionsMap(partitions))
	s.partitionMtx.Lock()
	defer s.partitionMtx.Unlock()

	for topic, parts := range partitions {
		tenant, virtualShard, err := s.codec.Decode(topic)
		// TODO: should propage more effectively
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to decode topic", "topic", topic, "err", err)
			continue
		}

		if _, ok := s.partitionHandlers[topic]; !ok {
			s.partitionHandlers[topic] = make(map[int32]*partitionProcessor)
		}

		for _, partition := range parts {
			processor := newPartitionProcessor(ctx, client, s.cfg.BuilderConfig, s.cfg.UploaderConfig, s.bucket, tenant, virtualShard, topic, partition, s.logger, s.reg, s.bufPool, s.cfg.IdleFlushTimeout, s.eventsProducerClient)
			s.partitionHandlers[topic][partition] = processor
			processor.start()
		}
	}
}

func (s *Service) handlePartitionsRevoked(partitions map[string][]int32) {
	level.Info(s.logger).Log("msg", "partitions revoked", "partitions", formatPartitionsMap(partitions))
	if s.State() == services.Stopping {
		// On shutdown, franz-go will send one more partitionRevoked event which we need to ignore to shutdown gracefully.
		return
	}
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
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if !s.cfg.IndexBuildingEnabled {
			return nil
		}
		builderCfg := indexobj.BuilderConfig{
			TargetObjectSize:        64 * 1024 * 1024, // 64MB
			TargetSectionSize:       16 * 1024 * 1024, // 16MB
			TargetPageSize:          128 * 1024,       // 128KB
			BufferSize:              64 * 1024 * 1024, // 64MB
			SectionStripeMergeLimit: 1024,
		}

		indexBuilder := indexing.NewIndexBuilder(ctx, s.logger, s.eventConsumerClient, builderCfg, s.cfg.UploaderConfig, s.bucket, "loki.metastore-events", s.reg, s.cfg.IndexBuildingEventsPerIndex, s.cfg.IndexStoragePrefix)
		indexBuilder.Start()
		defer indexBuilder.Stop()
		for {
			fetches := s.eventConsumerClient.PollRecords(ctx, -1)
			if fetches.IsClientClosed() || ctx.Err() != nil {
				return ctx.Err()
			}
			if errs := fetches.Errors(); len(errs) > 0 {
				level.Error(s.logger).Log("msg", "error fetching records", "err", errs)
				continue
			}
			if fetches.Empty() {
				continue
			}
			fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
				indexBuilder.Append(ftp.Records)
			})
		}
	})

	g.Go(func() error {
		if !s.cfg.PartitionProcessingEnabled {
			return nil
		}
		for {
			fetches := s.client.PollRecords(ctx, -1)
			if fetches.IsClientClosed() || ctx.Err() != nil {
				return nil
			}
			if errs := fetches.Errors(); len(errs) > 0 {
				var multiErr error
				for _, err := range errs {
					multiErr = errors.Join(multiErr, err.Err)
				}
				level.Error(s.logger).Log("msg", "error fetching records", "err", multiErr.Error())
				continue
			}
			if fetches.Empty() {
				continue
			}

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

				// Calculate total bytes in this batch
				var totalBytes int64
				for _, record := range records {
					totalBytes += int64(len(record.Value))
				}

				// Update metrics
				processor.metrics.addBytesProcessed(totalBytes)

				_ = processor.Append(records)
			})
		}
	})

	return g.Wait()
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
	level.Info(s.logger).Log("msg", "consumer stopped")
	return failureCase
}

// Helper function to format []int32 slice
func formatInt32Slice(slice []int32) string {
	if len(slice) == 0 {
		return "[]"
	}
	result := "["
	for i, v := range slice {
		if i > 0 {
			result += ","
		}
		result += strconv.Itoa(int(v))
	}
	result += "]"
	return result
}

// Helper function to format map[string][]int32 into a readable string
func formatPartitionsMap(partitions map[string][]int32) string {
	var result string
	for topic, parts := range partitions {
		if len(result) > 0 {
			result += ", "
		}
		result += topic + "=" + formatInt32Slice(parts)
	}
	return result
}
