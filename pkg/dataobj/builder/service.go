package builder

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// A partitionConsumer allows mocking of [kafkav2.SinglePartitionConsumer] in tests.
type partitionConsumer interface {
	services.Service
	SetInitialOffset(offset int64) error
}

// A resumeOffsetReader allows mocking of [kafkav2.OffsetReader] in tests.
type resumeOffsetReader interface {
	ResumeOffset(ctx context.Context, consumerGroup string, partition int32) (int64, error)
}

type Service struct {
	services.Service
	consumer           partitionConsumer
	offsetReader       resumeOffsetReader
	partition          int32
	consumerGroup      string
	processor          services.Service
	reporter           services.Service
	logger             log.Logger
	subservicesWatcher *services.FailureWatcher
}

func New(kafkaCfg kafka.Config, cfg Config, uploaderCfg dataobj_uploader.Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, reg prometheus.Registerer, logger log.Logger, overrides logsobj.TenantOverrides) (*Service, error) {
	const componentName = "dataobj-builder"
	logger = log.With(logger, "component", componentName)

	// Loki validates this config only when Kafka ingestion is enabled, so
	// validate it here too. For example, a flush concurrency of 0 would block
	// every flush.
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid dataobj builder config: %w", err)
	}

	// Each instance consumes exactly one partition, taken from the ordinal
	// suffix of its hostname (e.g. dataobj-builder-3 consumes partition 3),
	// unless cfg.PartitionID overrides it. The hostname is also used as the
	// Kafka consumer group.
	instanceID, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	partitionID := int32(cfg.PartitionID)
	if cfg.PartitionID < 0 {
		partitionID, err = partitionring.ExtractPartitionID(instanceID)
		if err != nil {
			return nil, fmt.Errorf("failed to extract partition ID from hostname: %w", err)
		}
	}

	// Set up the Kafka client that receives log entries. These entries are used to build
	// data objects.
	readerCfg := kafkaCfg
	readerCfg.Topic = cfg.Topic
	readerClient, err := client.NewReaderClient("loki.dataobj_builder", readerCfg, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for data topic: %w", err)
	}

	consumerGroup := instanceID
	offsetReader := kafkav2.NewOffsetReader(readerClient, cfg.Topic)
	committer := kafkav2.NewGroupCommitter(kadm.NewClient(readerClient), cfg.Topic, consumerGroup)
	records := make(chan *kgo.Record)
	consumer := kafkav2.NewSinglePartitionConsumer(
		readerClient,
		cfg.Topic,
		partitionID,
		kafkav2.OffsetStart, // We fetch the real initial offset before starting the service.
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_dataobj_builder_", reg),
	)
	// The name identifies the consumer in the failure that the builder returns.
	// It is set here because other services can share the kafkav2 package.
	consumer.WithName("dataobj-builder-consumer")
	uploader := dataobj_uploader.New(uploaderCfg, bucket, logger)
	if err := uploader.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register uploader metrics", "err", err)
	}

	builderMetrics := logsobj.NewBuilderMetrics()

	logsReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": fmt.Sprintf("%s.logs", componentName)}, reg)
	indexReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": fmt.Sprintf("%s.index", componentName)}, reg)
	tocReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": fmt.Sprintf("%s.toc", componentName)}, reg)
	err = builderMetrics.Register(logsReg)
	if err != nil {
		return nil, fmt.Errorf("failed to register logsobj builder metrics: %w", err)
	}

	// The dataobj-builder always drops duplicates for new objects
	cfg.LogsobjBuilder.DropDuplicates = true
	builderFactory, err := logsobj.NewBuilderFactory(cfg.LogsobjBuilder, scratchStore, builderMetrics, logger, overrides)
	if err != nil {
		return nil, fmt.Errorf("failed to create logsobj builder factory: %w", err)
	}
	sorter := logsobj.NewSorter(builderFactory, reg)
	flusher := newFlusher(sorter, uploader, logger, reg)

	idxBucket := objstore.NewPrefixedBucket(bucket, mCfg.IndexStoragePrefix)
	indexer, err := index.NewSimpleIndexer(
		cfg.IndexobjBuilder,
		scratchStore,
		logger,
		idxBucket,
		index.NewIndexerMetrics(indexReg),
		indexobj.NewBuilderMetrics(indexReg),
		index.NewCalculatorMetrics(indexReg),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	tocWriter := metastore.NewTableOfContentsWriter(
		idxBucket,
		metastore.DefaultTocWriterBackoffConfig,
		metastore.DefaultTocBuilderConfig,
		logger,
		metastore.NewTocWriterMetrics(tocReg),
		indexobj.NewBuilderMetrics(tocReg),
	)

	flushCommitter := newFlushCommitter(
		flusher,
		committer,
		indexer,
		tocWriter,
		partitionID,
		cfg.FlushConcurrency,
		logger,
		reg,
	)
	processor := newProcessor(
		NewMultiObjectBuilder(builderFactory, int(cfg.LogsobjBuilder.TargetObjectSize)),
		records,
		flushCommitter,
		cfg.IdleFlushTimeout,
		cfg.MaxBuilderAge,
		logger,
		reg,
	)

	// Partition 0 always has a builder, so its builder reports how many
	// builders the other partitions need. It derives the consumer groups of
	// the other builders from its own hostname. A partition override breaks
	// that mapping, so a builder with an override does not report.
	var reporter services.Service
	if partitionID == 0 {
		if cfg.PartitionID >= 0 {
			level.Info(logger).Log("msg", "not reporting required replicas, because the partition ID is set in the config")
		} else {
			partitionToConsumerGroup, err := newPartitionToConsumerGroupFunc(instanceID)
			if err != nil {
				return nil, fmt.Errorf("failed to derive the consumer groups of other builders: %w", err)
			}
			reporter = newRequiredReplicasReporter(
				offsetReader,
				partitionToConsumerGroup,
				cfg.RequiredReplicasRefreshInterval,
				logger,
				reg,
			)
		}
	}
	return newService(consumer, offsetReader, processor, reporter, partitionID, consumerGroup, logger), nil
}

// newService returns a Service that runs consumer and processor for the
// partition, and reporter if it is not nil. It fails if any of them fails.
// The consumer resumes from the last offset that consumerGroup committed.
func newService(
	consumer partitionConsumer,
	offsetReader resumeOffsetReader,
	processor services.Service,
	reporter services.Service,
	partition int32,
	consumerGroup string,
	logger log.Logger,
) *Service {
	s := &Service{
		consumer:           consumer,
		offsetReader:       offsetReader,
		partition:          partition,
		consumerGroup:      consumerGroup,
		processor:          processor,
		reporter:           reporter,
		logger:             logger,
		subservicesWatcher: services.NewFailureWatcher(),
	}
	// Watch the services before starting runs them. A failure that happens
	// before the watch starts is lost. The watcher keeps a failure until
	// running reads it.
	s.subservicesWatcher.WatchService(processor)
	s.subservicesWatcher.WatchService(consumer)
	if reporter != nil {
		s.subservicesWatcher.WatchService(reporter)
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s
}

// starting implements the Service interface's starting method.
func (s *Service) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting")
	if err := s.initResumeOffset(ctx); err != nil {
		return fmt.Errorf("failed to initialize offset for consumer: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.processor); err != nil {
		return fmt.Errorf("failed to start partition processor: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.consumer); err != nil {
		return fmt.Errorf("failed to start consumer: %w", err)
	}
	if s.reporter != nil {
		if err := services.StartAndAwaitRunning(ctx, s.reporter); err != nil {
			return fmt.Errorf("failed to start required replicas reporter: %w", err)
		}
	}
	return nil
}

// running implements the Service interface's running method.
func (s *Service) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.subservicesWatcher.Chan():
		return err
	}
}

// stopping implements the Service interface's stopping method.
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	ctx := context.TODO()
	if s.reporter != nil {
		if err := services.StopAndAwaitTerminated(ctx, s.reporter); err != nil {
			level.Warn(s.logger).Log("msg", "failed to stop required replicas reporter", "err", err)
		}
	}
	if err := services.StopAndAwaitTerminated(ctx, s.consumer); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop consumer", "err", err)
	}
	if err := services.StopAndAwaitTerminated(ctx, s.processor); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop partition processor", "err", err)
	}
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}

// initResumeOffset fetches and sets the resume offset (often the last committed
// offset) for the consumer. It must be called before starting the consumer.
func (s *Service) initResumeOffset(ctx context.Context) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 3,
	})
	var lastErr error
	for b.Ongoing() {
		initialOffset, err := s.offsetReader.ResumeOffset(ctx, s.consumerGroup, s.partition)
		if err == nil {
			lastErr = s.consumer.SetInitialOffset(initialOffset)
			break
		}
		lastErr = fmt.Errorf("failed to fetch resume offset: %w", err)
		b.Wait()
	}
	return lastErr
}
