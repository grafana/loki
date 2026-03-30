package consumer

import (
	"bytes"
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

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
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

	// recordsChan is set only in inmemory mode. Callers send *kgo.Record values
	// here instead of through Kafka.
	recordsChan chan *kgo.Record
}

// noopCommitter is a committer that does nothing. Used in inmemory mode where
// there is no Kafka offset to commit.
type noopCommitter struct{}

func (noopCommitter) Commit(_ context.Context, _ int32, _ int64) error { return nil }

// noopMetastoreEventEmitter is a metastoreEventEmitter that discards events.
// Used in inmemory mode where there is no Kafka metastore topic.
type noopMetastoreEventEmitter struct{}

func (noopMetastoreEventEmitter) Emit(_ context.Context, _ string, _ time.Time) error { return nil }

// tocFlusher wraps a flusher and builds an inline index after each log object flush.
// Used in inmemory mode so that flushed objects are queryable via ObjectMetastore
// without a Kafka-based index builder.
type tocFlusher struct {
	inner           flusher
	logBucket       objstore.Bucket                  // raw bucket for reading uploaded log objects
	logTocWriter    *metastore.TableOfContentsWriter // writes log-object ToC (for DataObjects / label queries)
	indexCalculator *dataobjindex.Calculator         // builds index from each log object
	indexUploader   uploader                         // uploads index objects to the index-prefixed bucket
	indexTocWriter  *metastore.TableOfContentsWriter // writes index-object ToC (for Sections queries)
	logger          log.Logger
}

func (f *tocFlusher) Flush(ctx context.Context, b builder, reason string) (string, error) {
	level.Info(f.logger).Log("msg", "tocFlusher.Flush called", "reason", reason)

	// Capture time ranges BEFORE the inner flush resets the builder.
	logTimeRanges := b.TimeRanges()

	objectPath, err := f.inner.Flush(ctx, b, reason)
	if err != nil {
		return "", err
	}

	// Write the log-object ToC entry so DataObjects() can discover it.
	if err := f.logTocWriter.WriteEntry(ctx, objectPath, logTimeRanges); err != nil {
		return objectPath, fmt.Errorf("failed to write log ToC entry: %w", err)
	}

	// Build an inline index from the uploaded log object so that Sections()
	// queries work without a separate Kafka-based index builder.
	logObj, err := dataobj.FromBucket(ctx, f.logBucket, objectPath, 0)
	if err != nil {
		return objectPath, fmt.Errorf("failed to read log object for indexing %s: %w", objectPath, err)
	}

	f.indexCalculator.Reset()
	if err := f.indexCalculator.Calculate(ctx, f.logger, logObj, objectPath); err != nil {
		return objectPath, fmt.Errorf("failed to calculate index for %s: %w", objectPath, err)
	}

	// Capture index time ranges before Flush resets the calculator.
	indexTimeRanges := f.indexCalculator.TimeRanges()

	indexObj, indexCloser, err := f.indexCalculator.Flush()
	if err != nil {
		return objectPath, fmt.Errorf("failed to flush index for %s: %w", objectPath, err)
	}
	defer indexCloser.Close()

	indexPath, err := f.indexUploader.Upload(ctx, indexObj)
	if err != nil {
		return objectPath, fmt.Errorf("failed to upload index for %s: %w", objectPath, err)
	}

	if err := f.indexTocWriter.WriteEntry(ctx, indexPath, indexTimeRanges); err != nil {
		return objectPath, fmt.Errorf("failed to write index ToC entry for %s: %w", objectPath, err)
	}

	return objectPath, nil
}

// RecordsChannel returns the in-process channel that callers use to submit
// records in inmemory mode. It is nil in Kafka mode.
func (s *Service) RecordsChannel() chan *kgo.Record {
	return s.recordsChan
}

// CheckReady returns nil if the service is running and ready to handle
// requests. Used by the readiness handler in loki.go to gate /ready.
func (s *Service) CheckReady(_ context.Context) error {
	if s.State() != services.Running {
		return fmt.Errorf("dataobj consumer is not running (state: %s)", s.State())
	}
	return nil
}

// NewInMemory creates a consumer Service that receives records via an in-process
// buffered channel instead of from Kafka. cfg.IngestMode must be IngestModeInMemory.
// mCfg is used to determine the index storage prefix so that flushed objects are
// queryable via ObjectMetastore.
func NewInMemory(cfg Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, reg prometheus.Registerer, logger log.Logger) (*Service, error) {
	if cfg.IngestMode != IngestModeInMemory {
		return nil, fmt.Errorf("NewInMemory requires IngestMode=%q, got %q", IngestModeInMemory, cfg.IngestMode)
	}
	logger = log.With(logger, "component", "dataobj-consumer-inmemory")

	const partitionID = int32(0)
	recordsChan := make(chan *kgo.Record, cfg.ChannelSize)

	s := &Service{
		cfg:         cfg,
		logger:      logger,
		reg:         reg,
		partition:   partitionID,
		recordsChan: recordsChan,
	}

	uploader := dataobj_uploader.New(cfg.UploaderConfig, bucket, logger)
	if err := uploader.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register uploader metrics", "err", err)
	}
	builderFactory := logsobj.NewBuilderFactory(cfg.BuilderConfig, scratchStore)
	sorter := logsobj.NewSorter(builderFactory, reg)
	s.flusher = newFlusher(sorter, uploader, logger, reg)

	// In inmemory mode there is no Kafka metastore consumer or index builder,
	// so we build a per-flush inline index and write both log ToC and index ToC
	// entries directly, making flushed objects queryable via ObjectMetastore.
	logTocWriter := metastore.NewTableOfContentsWriter(bucket, logger)

	// The index bucket uses the same prefix as ObjectMetastore so that
	// GetIndexes() finds the index objects we upload.
	indexBucket := objstore.NewPrefixedBucket(bucket, mCfg.IndexStoragePrefix)
	indexUploader := dataobj_uploader.New(cfg.UploaderConfig, indexBucket, logger)
	indexTocWriter := metastore.NewTableOfContentsWriter(indexBucket, logger)

	// Pre-create the tocs directory by uploading a zero-byte sentinel file.
	// The filesystem bucket's GetAndReplace (used by TableOfContentsWriter) does NOT
	// create parent directories, but Upload does. This is a no-op on cloud providers.
	if err := indexBucket.Upload(context.Background(), "tocs/.init", bytes.NewReader(nil)); err != nil {
		level.Debug(logger).Log("msg", "failed to pre-create index tocs directory", "err", err)
	}
	if err := bucket.Upload(context.Background(), "tocs/.init", bytes.NewReader(nil)); err != nil {
		level.Debug(logger).Log("msg", "failed to pre-create log tocs directory", "err", err)
	}

	// indexobj.Builder config: use the same base config as the log builder.
	indexBuilder, err := indexobj.NewBuilder(cfg.BuilderBaseConfig, scratchStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create index builder: %w", err)
	}

	wrappedFlusher := &tocFlusher{
		inner:           s.flusher,
		logBucket:       bucket,
		logTocWriter:    logTocWriter,
		indexCalculator: dataobjindex.NewCalculator(indexBuilder),
		indexUploader:   indexUploader,
		indexTocWriter:  indexTocWriter,
		logger:          logger,
	}

	wrapped := prometheus.WrapRegistererWith(prometheus.Labels{
		"partition": strconv.Itoa(int(partitionID)),
	}, reg)
	builder, err := builderFactory.NewBuilder(wrapped)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize data object builder: %w", err)
	}

	flushCommitter := newFlushCommitter(
		wrappedFlusher,
		noopMetastoreEventEmitter{},
		noopCommitter{},
		partitionID,
		logger,
		wrapped,
	)
	s.processor = newProcessor(
		builder,
		recordsChan,
		flushCommitter,
		cfg.IdleFlushTimeout,
		cfg.MaxBuilderAge,
		logger,
		wrapped,
	)

	s.Service = services.NewBasicService(s.inMemoryStarting, s.running, s.inMemoryStopping)
	return s, nil
}

func (s *Service) inMemoryStarting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting inmemory dataobj consumer")
	if err := services.StartAndAwaitRunning(ctx, s.processor); err != nil {
		return fmt.Errorf("failed to start partition processor: %w", err)
	}
	return nil
}

func (s *Service) inMemoryStopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping inmemory dataobj consumer")
	ctx := context.TODO()
	if err := services.StopAndAwaitTerminated(ctx, s.processor); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop partition processor", "err", err)
	}
	level.Info(s.logger).Log("msg", "stopped inmemory dataobj consumer")
	return failureCase
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
	flushCommitter := newFlushCommitter(
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
