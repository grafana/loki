package consumer

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
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
	lifecycler                  *ring.Lifecycler
	partitionInstanceLifecycler *ring.PartitionInstanceLifecycler
	consumer                    *kafkav2.SinglePartitionConsumer
	offsetReader                *kafkav2.OffsetReader
	partition                   int32
	processor                   *processor
	flusher                     *flusherImpl
	downscalePermitted          downscalePermittedFunc
	watcher                     *services.FailureWatcher
	logger                      log.Logger
	reg                         prometheus.Registerer
}

func New(kafkaCfg kafka.Config, cfg Config, idxCfg index.Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, _ string, _ ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger, overrides logsobj.TenantOverrides) (*Service, error) {
	logger = log.With(logger, "component", "dataobj-consumer")

	s := &Service{
		cfg:    cfg,
		logger: logger,
		reg:    reg,
	}

	// Set up the ring.
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

	// Set up the partition ring. Each instance of a dataobj consumer is responsible
	// for consuming exactly one partition, determined by its partition ID.
	// Once ready, the instance will declare its partition as active in the partition
	// ring. This is how distributors know which partitions can receive records and
	// which partitions can not (for example, we dont' want to send new records to
	// a dataobj consumer that is about to scale down).
	instanceID := cfg.LifecyclerConfig.ID
	partitionID, err := partitionring.ExtractPartitionID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to extract partition ID from lifecycler configuration: %w", err)
	}
	s.partition = partitionID
	// The mock KV is used in tests. If this is not a test then we must initialize
	// a real kv.
	partitionRingKV := cfg.PartitionRingConfig.KVStore.Mock
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
		cfg.PartitionRingConfig.ToLifecyclerConfig(partitionID, instanceID),
		PartitionRingName,
		PartitionRingKey,
		partitionRingKV,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_", reg))
	s.partitionInstanceLifecycler = partitionInstanceLifecycler

	// Set up the Kafka client that receives log entries. These entries are used to build
	// data objects.
	readerCfg := kafkaCfg
	readerCfg.Topic = cfg.Topic
	readerClient, err := client.NewReaderClient("loki.dataobj_consumer", readerCfg, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for data topic: %w", err)
	}

	s.offsetReader = kafkav2.NewOffsetReader(readerClient, cfg.Topic, instanceID, logger)
	committer := kafkav2.NewGroupCommitter(kadm.NewClient(readerClient), cfg.Topic, instanceID)
	records := make(chan *kgo.Record)
	s.consumer = kafkav2.NewSinglePartitionConsumer(
		readerClient,
		cfg.Topic,
		partitionID,
		kafkav2.OffsetStart, // We fetch the real initial offset before starting the service.
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_dataobj_consumer_", reg),
	)
	uploader := dataobj_uploader.New(cfg.UploaderConfig, bucket, logger)
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
	builderFactory, err := logsobj.NewBuilderFactory(cfg.BuilderConfig, scratchStore, builderMetrics, logger, overrides)
	if err != nil {
		return nil, fmt.Errorf("failed to create logsobj builder factory: %w", err)
	}
	sorter := logsobj.NewSorter(builderFactory, reg)
	s.flusher = newFlusher(sorter, uploader, logger, reg)

	idxBuilder, err := indexobj.NewBuilder(idxCfg.BuilderBaseConfig, scratchStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create index builder: %w", err)
	}
	if err := idxBuilder.RegisterMetrics(indexReg); err != nil {
		return nil, fmt.Errorf("failed to register indexobj builder metrics: %w", err)
	}

	calculator := index.NewCalculator(idxBuilder)
	if err := calculator.RegisterMetrics(indexReg); err != nil {
		return nil, fmt.Errorf("failed to register index calculator metrics: %w", err)
	}

	indexer, err := index.NewSimpleIndexer(
		calculator,
		logger,
		objstore.NewPrefixedBucket(bucket, mCfg.IndexStoragePrefix),
		indexReg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}
	flushCommitter := newFlushCommitter(
		s.flusher,
		committer,
		indexer,
		partitionID,
		logger,
		wrapped,
	)
	s.processor = newProcessor(
		NewTOCAlignedMultiBuilder(builderFactory, int(cfg.TargetObjectSize)),
		records,
		flushCommitter,
		cfg.IdleFlushTimeout,
		cfg.MaxBuilderAge,
		logger,
		wrapped,
	)
	s.downscalePermitted = newOffsetCommittedDownscaleFunc(s.offsetReader, partitionID, logger)

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
	if err := s.initResumeOffset(ctx); err != nil {
		return fmt.Errorf("failed to initialize offset for consumer: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.lifecycler); err != nil {
		return fmt.Errorf("failed to start lifecycler: %w", err)
	}
	if err := services.StartAndAwaitRunning(ctx, s.partitionInstanceLifecycler); err != nil {
		return fmt.Errorf("failed to start partition instance lifecycler: %w", err)
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
	<-ctx.Done()
	return nil
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
	if err := services.StopAndAwaitTerminated(ctx, s.partitionInstanceLifecycler); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop partition instance lifecycler", "err", err)
	}
	if err := services.StopAndAwaitTerminated(ctx, s.lifecycler); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop lifecycler", "err", err)
	}
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}

// Flush implements the [ring.FlushTransferer] interface.
func (s *Service) Flush() {}

// TransferOut implements the [ring.FlushTransferer] interface.
func (s *Service) TransferOut(_ context.Context) error {
	return nil
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
