package generator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits"
	frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *Generator) sendStreamMetadata(ctx context.Context, streamsBatch []distributor.KeyedStream, streamIdx int, batchSize int, tenant string, errCh chan error) {
	client, err := s.getFrontendClient()
	if err != nil {
		errCh <- fmt.Errorf("failed to get ingest limits frontend client (tenant: %s, stream_idx: %d, batch_size: %d): %w", tenant, streamIdx, batchSize, err)
		return
	}

	userCtx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(ctx, tenant))
	if err != nil {
		errCh <- fmt.Errorf("failed to inject user context (tenant: %s, stream_idx: %d, batch_size: %d): %w", tenant, streamIdx, batchSize, err)
		return
	}

	var streamMetadata []*proto.StreamMetadata
	for _, stream := range streamsBatch {
		streamMetadata = append(streamMetadata, &proto.StreamMetadata{
			StreamHash: stream.HashKeyNoShard,
		})
	}

	req := &proto.ExceedsLimitsRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}

	// Check if the stream exceeds limits
	if client == nil {
		errCh <- fmt.Errorf("no ingest limits frontend client (tenant: %s, stream_idx: %d, batch_size: %d)", tenant, streamIdx, batchSize)
		return
	}

	resp, err := client.ExceedsLimits(userCtx, req)
	if err != nil {
		errCh <- fmt.Errorf("failed to check if stream exceeds limits (tenant: %s, stream_idx: %d, batch_size: %d): %w", tenant, streamIdx, batchSize, err)
		return
	}

	switch {
	case len(resp.Results) > 0:
		var results string
		reasonCounts := make(map[string]int)
		for _, rejectedStream := range resp.Results {
			reason := limits.Reason(rejectedStream.Reason).String()
			reasonCounts[reason]++
		}
		for reason, count := range reasonCounts {
			results += fmt.Sprintf("%s: %d, ", reason, count)
		}

		level.Info(s.logger).Log("msg", "Stream exceeds limits", "tenant", tenant, "batch_size", batchSize, "stream_idx", streamIdx, "rejected", results)
		return
	case len(resp.Results) == 0:
		level.Debug(s.logger).Log("msg", "Stream accepted", "tenant", tenant, "batch_size", batchSize, "stream_idx", streamIdx)
	}

	// Send single stream to Kafka
	s.sendStreamsToKafka(ctx, streamsBatch, tenant, errCh)
	level.Debug(s.logger).Log("msg", "Sent streams to Kafka", "tenant", tenant, "batch_size", batchSize, "stream_idx", streamIdx)
}

func (s *Generator) sendStreamsToKafka(ctx context.Context, streams []distributor.KeyedStream, tenant string, errCh chan error) {
	for _, stream := range streams {
		go func(stream distributor.KeyedStream) {
			partitionID := int32(stream.HashKeyNoShard % uint64(s.cfg.NumPartitions))

			startTime := time.Now()

			// Calculate log size from actual entries
			var logSize uint64
			for _, entry := range stream.Stream.Entries {
				logSize += uint64(len(entry.Line))
			}

			metadata := proto.StreamMetadata{
				StreamHash: stream.HashKeyNoShard,
				TotalSize:  logSize,
			}
			b, err := metadata.Marshal()
			if err != nil {
				errCh <- fmt.Errorf("failed to marshal metadata: %w", err)
				return
			}

			metadataRecord := &kgo.Record{
				Key:       []byte(tenant),
				Value:     b,
				Partition: partitionID,
				Topic:     fmt.Sprintf("%s.metadata", s.cfg.Kafka.Topic),
			}
			// Send to Kafka
			produceResults := s.writer.ProduceSync(ctx, []*kgo.Record{metadataRecord})

			if count, sizeBytes := successfulProduceRecordsStats(produceResults); count > 0 {
				s.metrics.kafkaWriteLatency.Observe(time.Since(startTime).Seconds())
				s.metrics.kafkaWriteBytesTotal.Add(float64(sizeBytes))
			}

			// Check for errors
			for _, result := range produceResults {
				if result.Err != nil {
					errCh <- fmt.Errorf("failed to write stream metadata to kafka: %w", result.Err)
					return
				}
			}
		}(stream)
	}
}

func successfulProduceRecordsStats(results kgo.ProduceResults) (count, sizeBytes int) {
	for _, res := range results {
		if res.Err == nil && res.Record != nil {
			count++
			sizeBytes += len(res.Record.Value)
		}
	}

	return
}

var frontendReadOp = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

func (s *Generator) getFrontendClient() (*frontend_client.Client, error) {
	instances, err := s.frontendRing.GetAllHealthy(frontendReadOp)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingest limits frontend instances: %w", err)
	}

	rand.Shuffle(len(instances.Instances), func(i, j int) {
		instances.Instances[i], instances.Instances[j] = instances.Instances[j], instances.Instances[i]
	})

	client, err := s.frontentClientPool.GetClientForInstance(instances.Instances[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get ingest limits frontend client: %w", err)
	}

	return client.(*frontend_client.Client), nil
}

func newKafkaWriter(cfg kafka.Config, logger log.Logger, reg prometheus.Registerer) (*client.Producer, error) {
	// Create a new Kafka client with writer configuration
	// Using same settings as distributor for max inflight requests
	maxInflightProduceRequests := 20

	// Create the Kafka client
	kafkaClient, err := client.NewWriterClient("stream-generator", cfg, maxInflightProduceRequests, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	// Create a producer with 100MB buffer limit
	producer := client.NewProducer("stream-generator", kafkaClient, cfg.ProducerMaxBufferedBytes, reg)
	return producer, nil
}
