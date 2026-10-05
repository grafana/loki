package builder

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	ResumeOffset(ctx context.Context, partition int32) (int64, error)
}

type Service struct {
	services.Service
	consumer           partitionConsumer
	offsetReader       resumeOffsetReader
	partition          int32
	processor          services.Service
	downscalePermitted downscalePermittedFunc
	logger             log.Logger
	subservicesWatcher *services.FailureWatcher
}

func New(kafkaCfg kafka.Config, cfg Config, uploaderCfg dataobj_uploader.Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, reg prometheus.Registerer, logger log.Logger, overrides logsobj.TenantOverrides) (*Service, error) {
	logger = log.With(logger, "component", "dataobj-builder")

	// Each instance consumes exactly one partition, taken from the ordinal
	// suffix of its hostname (e.g. dataobj-builder-3 consumes partition 3).
	// The hostname is also used as the Kafka consumer group.
	instanceID, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	partitionID, err := partitionring.ExtractPartitionID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to extract partition ID from hostname: %w", err)
	}

	// Set up the Kafka client that receives log entries. These entries are used to build
	// data objects.
	readerCfg := kafkaCfg
	readerCfg.Topic = cfg.Topic
	readerClient, err := client.NewReaderClient("loki.dataobj_builder", readerCfg, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for data topic: %w", err)
	}

	offsetReader := kafkav2.NewOffsetReader(readerClient, cfg.Topic, instanceID, logger)
	committer := kafkav2.NewGroupCommitter(kadm.NewClient(readerClient), cfg.Topic, instanceID)
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
	wrapped := prometheus.WrapRegistererWith(prometheus.Labels{
		"partition": strconv.Itoa(int(partitionID)),
	}, reg)
	// The logs and index builders share the same section and encoding
	// collectors, so both must carry a component label. Registering only one of
	// them with it makes the label names differ for the same metric name, which
	// the Prometheus registry rejects.
	logsReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": "logs"}, wrapped)
	indexReg := prometheus.WrapRegistererWith(prometheus.Labels{"component": "index"}, wrapped)
	err = builderMetrics.Register(logsReg)
	if err != nil {
		return nil, fmt.Errorf("failed to register logsobj builder metrics: %w", err)
	}
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

	tocWriter := metastore.NewTableOfContentsWriter(idxBucket, logger)
	if err := tocWriter.RegisterMetrics(indexReg); err != nil {
		return nil, fmt.Errorf("failed to register Table of Contents writer metrics: %w", err)
	}

	flushCommitter := newFlushCommitter(
		flusher,
		committer,
		indexer,
		tocWriter,
		partitionID,
		logger,
		wrapped,
	)
	processor := newProcessor(
		NewMultiObjectBuilder(builderFactory, int(cfg.LogsobjBuilder.TargetObjectSize)),
		records,
		flushCommitter,
		cfg.IdleFlushTimeout,
		cfg.MaxBuilderAge,
		logger,
		wrapped,
	)

	downscalePermitted := newOffsetCommittedDownscaleFunc(offsetReader, partitionID, logger)
	return newService(consumer, offsetReader, processor, downscalePermitted, partitionID, logger), nil
}

// newService returns a Service that runs consumer and processor for the
// partition. It fails if either of them fails.
func newService(
	consumer partitionConsumer,
	offsetReader resumeOffsetReader,
	processor services.Service,
	downscalePermitted downscalePermittedFunc,
	partition int32,
	logger log.Logger,
) *Service {
	s := &Service{
		consumer:           consumer,
		offsetReader:       offsetReader,
		partition:          partition,
		processor:          processor,
		downscalePermitted: downscalePermitted,
		logger:             logger,
		subservicesWatcher: services.NewFailureWatcher(),
	}
	// Watch the services before starting runs them. A failure that happens
	// before the watch starts is lost. The watcher keeps a failure until
	// running reads it.
	s.subservicesWatcher.WatchService(processor)
	s.subservicesWatcher.WatchService(consumer)
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
		initialOffset, err := s.offsetReader.ResumeOffset(ctx, s.partition)
		if err == nil {
			lastErr = s.consumer.SetInitialOffset(initialOffset)
			break
		}
		lastErr = fmt.Errorf("failed to fetch resume offset: %w", err)
		b.Wait()
	}
	return lastErr
}
