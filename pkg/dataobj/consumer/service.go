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
	metastoreEvents             *kgo.Client
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
	builderFactory := logsobj.NewBuilderFactory(cfg.BuilderConfig, scratchStore)
	sorter := logsobj.NewSorter(builderFactory, reg)
	s.flusher = newFlusher(sorter, uploader, logger, reg)
	wrapped := prometheus.WrapRegistererWith(prometheus.Labels{
		"partition": strconv.Itoa(int(partitionID)),
	}, reg)
	builder, err := builderFactory.NewBuilder(wrapped)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize data object builder: %w", err)
	}
	flushManager := newFlushManager(
		s.flusher,
		newMetastoreEvents(partitionID, int32(mCfg.PartitionRatio), metastoreEvents),
		committer,
		partitionID,
		logger,
		wrapped,
	)
	s.processor = newProcessor(
		builder,
		records,
		flushManager,
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
