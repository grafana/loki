// Package chunkbuilder implements the chunk-builder, an experimental target
// that consumes one partition of a Kafka topic to build chunks and a TSDB index
// in object storage. Unlike an ingester, it keeps no WAL, enforces no limits
// and serves no reads.
//
// So far the chunk-builder only consumes and decodes records.
package chunkbuilder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
)

const componentName = "chunk-builder"

// A partitionConsumer allows mocking of [kafkav2.SinglePartitionConsumer] in
// tests.
type partitionConsumer interface {
	services.Service
	SetInitialOffset(offset int64) error
}

// A resumeOffsetReader allows mocking of [kafkav2.OffsetReader] in tests.
type resumeOffsetReader interface {
	ResumeOffset(ctx context.Context, consumerGroup string, partition int32) (int64, error)
}

// Service is the chunk-builder. It consumes one partition of a Kafka topic and
// processes each record.
type Service struct {
	services.Service

	consumer      partitionConsumer
	offsetReader  resumeOffsetReader
	records       <-chan *kgo.Record
	processor     *processor
	partition     int32
	consumerGroup string
	logger        log.Logger
	watcher       *services.FailureWatcher

	// closeClient closes the Kafka client of the consumer after the consumer
	// stops. It can be nil.
	closeClient func()
}

// New returns the chunk-builder for the partition that cfg names, or that the
// hostname implies. It consumes cfg.Topic with the rest of kafkaCfg.
func New(kafkaCfg kafka.Config, cfg Config, reg prometheus.Registerer, logger log.Logger) (*Service, error) {
	logger = log.With(logger, "component", componentName)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid chunk-builder config: %w", err)
	}
	// Loki validates the Kafka config only when the ingesters consume Kafka,
	// so validate it here. The chunk-builder topic replaces the ingester
	// topic, which the chunk-builder does not need.
	readerCfg := kafkaCfg
	readerCfg.Topic = cfg.Topic
	if err := readerCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid kafka config: %w", err)
	}

	partition, err := resolvePartitionID(cfg.PartitionID, os.Hostname)
	if err != nil {
		return nil, err
	}
	// The consumer adds the topic and partition to its own logs.
	partitionLogger := log.With(logger, "topic", cfg.Topic, "partition", partition)

	readerClient, err := client.NewReaderClient("loki.chunk_builder", readerCfg, partitionLogger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	records := make(chan *kgo.Record)
	consumer := kafkav2.NewSinglePartitionConsumer(
		readerClient,
		cfg.Topic,
		partition,
		kafkav2.OffsetStart, // starting replaces it with the resume offset.
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_chunk_builder_", reg),
	)
	// The name identifies the consumer in the failure that the service
	// returns, since other services use the kafkav2 package too.
	consumer.WithName("chunk-builder-consumer")

	s := newService(
		consumer,
		kafkav2.NewOffsetReader(readerClient, cfg.Topic),
		records,
		newProcessor(newMetrics(reg), partitionLogger),
		partition,
		cfg.ConsumerGroup,
		partitionLogger,
	)
	s.closeClient = readerClient.Close
	return s, nil
}

// resolvePartitionID returns configured if it is not negative. Otherwise it
// returns the ordinal suffix of the hostname, so chunk-builder-3 consumes
// partition 3.
func resolvePartitionID(configured int, hostname func() (string, error)) (int32, error) {
	if configured >= 0 {
		return int32(configured), nil
	}
	name, err := hostname()
	if err != nil {
		return 0, fmt.Errorf("failed to get hostname: %w", err)
	}
	partition, err := partitionring.ExtractPartitionID(name)
	if err != nil {
		return 0, fmt.Errorf("failed to extract partition ID from hostname: %w", err)
	}
	return partition, nil
}

// newService returns a Service that processes the records that consumer sends
// to records. The consumer resumes from the last offset that consumerGroup
// committed for the partition.
func newService(
	consumer partitionConsumer,
	offsetReader resumeOffsetReader,
	records <-chan *kgo.Record,
	processor *processor,
	partition int32,
	consumerGroup string,
	logger log.Logger,
) *Service {
	s := &Service{
		consumer:      consumer,
		offsetReader:  offsetReader,
		records:       records,
		processor:     processor,
		partition:     partition,
		consumerGroup: consumerGroup,
		logger:        logger,
		watcher:       services.NewFailureWatcher(),
	}
	// Watch the consumer before it starts, so no failure is lost. The watcher
	// keeps a failure until running reads it.
	s.watcher.WatchService(consumer)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping).WithName(componentName)
	return s
}

// starting implements [services.StartingFn].
func (s *Service) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting")
	if err := s.initResumeOffset(ctx); err != nil {
		s.close()
		return fmt.Errorf("failed to initialize the offset of the consumer: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.consumer); err != nil {
		s.close()
		return fmt.Errorf("failed to start the consumer: %w", err)
	}
	return nil
}

// running implements [services.RunningFn].
func (s *Service) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-s.watcher.Chan():
			return err
		case rec, ok := <-s.records:
			if !ok {
				return s.consumerStopped(ctx)
			}
			s.processor.process(rec, time.Now())
		}
	}
}

// consumerStopped returns the error that running returns after the consumer
// closed the records channel. The consumer closes it only when it stops, and
// only stopping stops it, so the consumer stopped on its own.
func (s *Service) consumerStopped(ctx context.Context) error {
	err := s.consumer.AwaitTerminated(ctx)
	if ctx.Err() != nil {
		// The service is stopping.
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("the consumer stopped unexpectedly")
}

// stopping implements [services.StoppingFn].
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	if err := services.StopAndAwaitTerminated(context.Background(), s.consumer); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop the consumer", "err", err)
	}
	s.close()
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}

// close closes the Kafka client, if the service owns one.
func (s *Service) close() {
	if s.closeClient != nil {
		s.closeClient()
	}
}

// initResumeOffset sets the offset that the consumer starts from to the offset
// after the last one that the consumer group committed. With no committed
// offset, the consumer starts from the start of the partition. It must run
// before the consumer starts.
//
// TODO(chunk-builders): Commit offsets. The chunk-builder must commit an offset
// only after a checkpoint holds every record up to it, that is after the chunks
// and index of those records are in object storage. A commit before that would
// mark records as consumed that no chunk holds. Until then, the consumer group
// has no committed offset, so every start consumes the partition from the
// start.
func (s *Service) initResumeOffset(ctx context.Context) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 3,
	})
	var lastErr error
	for b.Ongoing() {
		offset, err := s.offsetReader.ResumeOffset(ctx, s.consumerGroup, s.partition)
		if err == nil {
			level.Info(s.logger).Log("msg", "resuming", "consumer_group", s.consumerGroup, "offset", offset)
			return s.consumer.SetInitialOffset(offset)
		}
		lastErr = fmt.Errorf("failed to fetch the resume offset: %w", err)
		b.Wait()
	}
	if lastErr == nil {
		// The backoff stops without an attempt when ctx is already canceled.
		lastErr = b.Err()
	}
	return lastErr
}
