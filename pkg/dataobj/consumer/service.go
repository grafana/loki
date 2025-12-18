package consumer

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/scratch"
)

const (
	RingKey           = "dataobj-consumer"
	RingName          = "dataobj-consumer"
	PartitionRingKey  = "dataobj-consumer-partitions-key"
	PartitionRingName = "dataobj-consumer-partitions"
)

type Service struct {
	services.Service
	cfg                         Config
	metastoreEvents             *kgo.Client
	lifecycler                  *ring.Lifecycler
	partitionInstanceLifecycler *ring.PartitionInstanceLifecycler
	partitionReader             *partition.ReaderService
	downscalePermitted          downscalePermittedFunc
	watcher                     *services.FailureWatcher
	logger                      log.Logger
	reg                         prometheus.Registerer
}

func New(kafkaCfg kafka.Config, cfg Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, _ string, _ ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) (*Service, error) {
	logger = log.With(logger, "component", "dataobj-consumer")

	s := &Service{
		cfg:    cfg,
		logger: logger,
		reg:    reg,
	}

	// Set up the Kafka client that produces events for the metastore. This
	// must be done before we can set up the client that consumes records
	// from distributors, as the code that consumes these records from also
	// needs to be able to produce metastore events.
	metastoreEventsCfg := kafkaCfg
	metastoreEventsCfg.Topic = "loki.metastore-events"
	metastoreEventsCfg.AutoCreateTopicDefaultPartitions = 1
	metastoreEvents, err := client.NewWriterClient("loki.metastore-events", metastoreEventsCfg, 50, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for metastore events topic: %w", err)
	}
	s.metastoreEvents = metastoreEvents

	lifecycler, err := ring.NewLifecycler(
		cfg.LifecyclerConfig,
		s,
		RingName,
		RingKey,
		false,
		logger,
		prometheus.WrapRegistererWithPrefix("dataobj-consumer_", reg),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	s.lifecycler = lifecycler

	// An instance must register itself in the partition ring. Each instance
	// is responsible for consuming exactly one partition determined by
	// its partition ID. Once ready, the instance will declare its partition
	// as active in the partition ring. This is how distributors know which
	// partitions have a ready consumer.
	partitionID, err := partitionring.ExtractPartitionID(cfg.LifecyclerConfig.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to extract partition ID from lifecycler configuration: %w", err)
	}
	partitionRingKV := cfg.PartitionRingConfig.KVStore.Mock
	// The mock KV is used in tests. If this is not a test then we must
	// initialize a real kv.
	if partitionRingKV == nil {
		partitionRingKV, err = kv.NewClient(
			cfg.PartitionRingConfig.KVStore,
			ring.GetPartitionRingCodec(),
			kv.RegistererWithKVName(reg, "dataobj-consumer-lifecycler"),
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to set up partition ring: %w", err)
		}
	}
	partitionInstanceLifecycler := ring.NewPartitionInstanceLifecycler(
		cfg.PartitionRingConfig.ToLifecyclerConfig(partitionID, cfg.LifecyclerConfig.ID),
		PartitionRingName,
		PartitionRingKey,
		partitionRingKV,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_", reg))
	s.partitionInstanceLifecycler = partitionInstanceLifecycler

	processorFactory := newPartitionProcessorFactory(
		cfg,
		mCfg,
		metastoreEvents,
		bucket,
		scratchStore,
		logger,
		reg,
		cfg.Topic,
		partitionID,
	)
	kafkaCfg.Topic = cfg.Topic
	partitionReader, err := partition.NewReaderService(
		kafkaCfg,
		partitionID,
		cfg.LifecyclerConfig.ID,
		processorFactory.New,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_dataobj_consumer_", reg),
		partitionInstanceLifecycler,
	)
	if err != nil {
		return nil, err
	}
	s.partitionReader = partitionReader

	// TODO: We have to pass prometheus.NewRegistry() to avoid duplicate
	// metric registration with partition.NewReaderService.
	offsetManager, err := partition.NewKafkaOffsetManager(kafkaCfg, cfg.LifecyclerConfig.ID, logger, prometheus.NewRegistry())
	if err != nil {
		return nil, err
	}
	s.downscalePermitted = newOffsetCommittedDownscaleFunc(offsetManager, partitionID, logger)

	watcher := services.NewFailureWatcher()
	watcher.WatchService(lifecycler)
	watcher.WatchService(partitionInstanceLifecycler)
	s.watcher = watcher

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

// starting implements the Service interface's starting method.
func (s *Service) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting")
	if err := services.StartAndAwaitRunning(ctx, s.lifecycler); err != nil {
		return fmt.Errorf("failed to start lifecycler: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.partitionInstanceLifecycler); err != nil {
		return fmt.Errorf("failed to start partition instance lifecycler: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.partitionReader); err != nil {
		return fmt.Errorf("failed to start partition reader: %w", err)
	}
	return nil
}

// running implements the Service interface's running method.
func (s *Service) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements the Service interface's stopping method.
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	ctx := context.TODO()
	if err := services.StopAndAwaitTerminated(ctx, s.partitionReader); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop partition reader", "err", err)
	}
	if err := services.StopAndAwaitTerminated(ctx, s.partitionInstanceLifecycler); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop partition instance lifecycler", "err", err)
	}
	if err := services.StopAndAwaitTerminated(ctx, s.lifecycler); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop lifecycler", "err", err)
	}
	s.metastoreEvents.Close()
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}

// Flush implements the [ring.FlushTransferer] interface.
func (s *Service) Flush() {}

// TransferOut implements the [ring.FlushTransferer] interface.
func (s *Service) TransferOut(_ context.Context) error {
	return nil
}
