package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var ErrPartitionRevoked = errors.New("partition revoked")

type triggerType string

const (
	triggerTypeNormal triggerType = "normal"
	triggerTypeFlush  triggerType = "flush"
)

type Config struct {
	indexobj.BuilderConfig `yaml:",inline"`
	EventsPerIndex         int           `yaml:"events_per_index" experimental:"true"`
	FlushInterval          time.Duration `yaml:"flush_interval" experimental:"true"`
	MaxIdleTime            time.Duration `yaml:"max_idle_time" experimental:"true"`
	MinFlushEvents         int           `yaml:"min_flush_events" experimental:"true"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.IntVar(&cfg.EventsPerIndex, prefix+"events-per-index", 32, "Experimental: The number of events to batch before building an index")
	f.DurationVar(&cfg.FlushInterval, prefix+"flush-interval", 10*time.Second, "Experimental: How often to check for stale partitions to flush")
	f.DurationVar(&cfg.MaxIdleTime, prefix+"max-idle-time", 30*time.Second, "Experimental: Maximum time to wait before flushing buffered events")
	f.IntVar(&cfg.MinFlushEvents, prefix+"min-flush-events", 1, "Experimental: Minimum number of events required to trigger a flush")
}

type bufferedEvent struct {
	event  metastore.ObjectWrittenEvent
	record *kgo.Record
}

type partitionState struct {
	events       []bufferedEvent
	lastActivity time.Time
	isProcessing bool
}

type downloadedObject struct {
	event       metastore.ObjectWrittenEvent
	objectBytes *[]byte
	err         error
}

const (
	indexConsumerGroup = "index-builder"
)

// An interface for the methods needed from a calculator. Useful for testing.
type calculator interface {
	Calculate(context.Context, log.Logger, *dataobj.Object, string) error
	Flush() (*dataobj.Object, io.Closer, error)
	TimeRanges() []multitenancy.TimeRange
	Reset()
}

// An interface for the methods needed from a kafka client. Useful for testing.
type kafkaClient interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	Close()
}

type Builder struct {
	services.Service

	cfg  Config
	mCfg metastore.Config

	// Kafka client and topic/partition info
	client kafkaClient
	topic  string

	// Processing pipeline
	downloadQueue     chan metastore.ObjectWrittenEvent
	downloadedObjects chan downloadedObject
	calculator        calculator
	tocWriter         *metastore.TableOfContentsWriter

	partitionStates map[int32]*partitionState
	flushTicker     *time.Ticker

	// Builder initialization
	builderCfg         indexobj.BuilderConfig
	objectBucket       objstore.Bucket
	indexStorageBucket objstore.Bucket // The bucket to store the indexes might not be the same one as where we read the objects from
	scratchStore       scratch.Store

	// Metrics
	metrics *indexBuilderMetrics

	// Control and coordination
	ctx                        context.Context
	cancel                     context.CancelCauseFunc
	wg                         sync.WaitGroup
	logger                     log.Logger
	activeCalculationPartition int32
	cancelActiveCalculation    context.CancelCauseFunc
	partitionsMutex            sync.Mutex
}

func NewIndexBuilder(
	cfg Config,
	mCfg metastore.Config,
	kafkaCfg kafka.Config,
	logger log.Logger,
	instanceID string,
	bucket objstore.Bucket,
	scratchStore scratch.Store,
	reg prometheus.Registerer,
) (*Builder, error) {
	builderReg := prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     kafkaCfg.Topic,
		"component": "index_builder",
	}, reg)

	metrics := newIndexBuilderMetrics()
	if err := metrics.register(builderReg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	builder, err := indexobj.NewBuilder(cfg.BuilderConfig, scratchStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create index builder: %w", err)
	}
	calculator := NewCalculator(builder)

	indexStorageBucket := objstore.NewPrefixedBucket(bucket, mCfg.IndexStoragePrefix)
	tocWriter := metastore.NewTableOfContentsWriter(indexStorageBucket, logger)

	if err := builder.RegisterMetrics(builderReg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	// Set up queues to download the next object (I/O bound) while processing the current one (CPU bound) in order to maximize throughput.
	// Setting the channel buffer sizes caps the total memory usage by only keeping up to 3 objects in memory at a time: One being processed, one fully downloaded and one being downloaded from the queue.
	downloadQueue := make(chan metastore.ObjectWrittenEvent, cfg.EventsPerIndex)
	downloadedObjects := make(chan downloadedObject, 1)

	s := &Builder{
		cfg:                cfg,
		mCfg:               mCfg,
		logger:             logger,
		objectBucket:       bucket,
		indexStorageBucket: indexStorageBucket,
		tocWriter:          tocWriter,
		downloadedObjects:  downloadedObjects,
		downloadQueue:      downloadQueue,
		metrics:            metrics,
		calculator:         calculator,
		partitionStates:    make(map[int32]*partitionState),
	}

	kafkaCfg.AutoCreateTopicEnabled = true
	eventConsumerClient, err := client.NewReaderClient(
		"index_builder",
		kafkaCfg,
		logger,
		reg,
		kgo.ConsumeTopics(kafkaCfg.Topic),
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.ConsumerGroup(indexConsumerGroup),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsRevoked(s.handlePartitionsRevoked),
		kgo.OnPartitionsAssigned(s.handlePartitionsAssigned),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer client: %w", err)
	}
	s.client = eventConsumerClient

	s.Service = services.NewBasicService(nil, s.run, s.stopping)

	return s, nil
}

// bufferAndTryProcess is the unified method that handles both buffering and processing decisions
func (p *Builder) bufferAndTryProcess(partition int32, newEvent *bufferedEvent, trigger triggerType) (context.Context, []bufferedEvent) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	state, exists := p.partitionStates[partition]
	if !exists {
		return nil, nil
	}

	// Add new event to buffer if provided (normal processing case)
	if newEvent != nil {
		state.events = append(state.events, *newEvent)
		state.lastActivity = time.Now()
		level.Debug(p.logger).Log("msg", "buffered new event for partition", "count", len(state.events), "partition", partition)
	}

	// Check if we can start processing
	if state.isProcessing || len(state.events) == 0 {
		return nil, nil
	}

	// Check trigger-specific requirements
	switch trigger {
	case triggerTypeNormal:
		if len(state.events) < p.cfg.EventsPerIndex {
			return nil, nil
		}
	case triggerTypeFlush:
		if len(state.events) < p.cfg.MinFlushEvents {
			return nil, nil
		}
		if time.Since(state.lastActivity) < p.cfg.MaxIdleTime {
			return nil, nil
		}
	default:
		level.Error(p.logger).Log("msg", "unknown trigger type", "trigger", string(trigger))
		return nil, nil
	}

	// Atomically mark as processing and extract events
	state.isProcessing = true
	eventsToProcess := make([]bufferedEvent, len(state.events))
	copy(eventsToProcess, state.events)

	// Set up cancellation context
	ctx, cancel := context.WithCancelCause(p.ctx)
	p.activeCalculationPartition = partition
	p.cancelActiveCalculation = cancel

	level.Debug(p.logger).Log("msg", "started processing partition",
		"partition", partition, "events", len(eventsToProcess), "trigger", string(trigger))

	return ctx, eventsToProcess
}

func (p *Builder) handlePartitionsAssigned(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			p.partitionStates[partition] = &partitionState{
				events:       make([]bufferedEvent, 0),
				lastActivity: time.Now(),
				isProcessing: false,
			}
		}
	}
}

// This is not thread-safe
func (p *Builder) handlePartitionsRevoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			delete(p.partitionStates, partition)
			if p.activeCalculationPartition == partition && p.cancelActiveCalculation != nil {
				p.cancelActiveCalculation(ErrPartitionRevoked)
			}
		}
	}
}

func (p *Builder) run(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancelCause(ctx)

	p.wg.Add(1)
	go func() {
		// Download worker
		defer p.wg.Done()
		for event := range p.downloadQueue {
			objLogger := log.With(p.logger, "object_path", event.ObjectPath)
			downloadStart := time.Now()

			objectReader, err := p.objectBucket.Get(p.ctx, event.ObjectPath)
			if err != nil {
				p.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to fetch object from storage: %w", err),
				}
				continue
			}

			object, err := io.ReadAll(objectReader)
			_ = objectReader.Close()
			if err != nil {
				p.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to read object: %w", err),
				}
				continue
			}
			level.Info(objLogger).Log("msg", "downloaded object", "duration", time.Since(downloadStart), "size_mb", float64(len(object))/1024/1024, "avg_speed_mbps", float64(len(object))/time.Since(downloadStart).Seconds()/1024/1024)
			p.downloadedObjects <- downloadedObject{
				event:       event,
				objectBytes: &object,
			}
		}
	}()

	// Start flush worker if configured
	if p.cfg.FlushInterval > 0 {
		p.flushTicker = time.NewTicker(p.cfg.FlushInterval)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer p.flushTicker.Stop()

			for {
				select {
				case <-p.flushTicker.C:
					p.checkAndFlushStalePartitions()
				case <-p.ctx.Done():
					return
				}
			}
		}()
	}

	level.Info(p.logger).Log("msg", "started index builder service")
	for {
		fetches := p.client.PollRecords(ctx, -1)
		if err := fetches.Err0(); err != nil {
			if errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
				return err
			}
			// Some other error occurred. We will check it in
			// [processFetchTopicPartition] instead.
		}
		if fetches.Empty() {
			continue
		}
		fetches.EachPartition(func(fetch kgo.FetchTopicPartition) {
			if err := fetch.Err; err != nil {
				level.Error(p.logger).Log("msg", "failed to fetch records for topic partition", "topic", fetch.Topic, "partition", fetch.Partition, "err", err.Error())
				return
			}
			// TODO(benclive): Verify if we need to return re-poll ASAP or if sequential processing is good enough.
			for _, record := range fetch.Records {
				p.processRecord(record)
			}
		})
	}
}

func (p *Builder) stopping(failureCase error) error {
	close(p.downloadQueue)
	p.cancel(failureCase)
	p.wg.Wait()
	close(p.downloadedObjects)
	p.client.Close()
	return nil
}

// processRecord processes a single record. It is not safe for concurrent use.
func (p *Builder) processRecord(record *kgo.Record) {
	calculationCtx, eventsToIndex := p.appendRecord(record)
	if len(eventsToIndex) == 0 {
		return
	}

	defer p.cleanupPartition(record.Partition)

	// Build the index.
	events := make([]metastore.ObjectWrittenEvent, len(eventsToIndex))
	for i, buffered := range eventsToIndex {
		events[i] = buffered.event
	}

	err := p.buildIndex(calculationCtx, events)
	if err != nil {
		if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
			level.Debug(p.logger).Log("msg", "partition revoked, aborting index build", "partition", record.Partition)
			return
		}
		level.Error(p.logger).Log("msg", "failed to build index", "err", err, "partition", record.Partition)
		return
	}

	// Commit all records from this batch
	records := make([]*kgo.Record, len(eventsToIndex))
	for i, buffered := range eventsToIndex {
		records[i] = buffered.record
	}

	if err := p.commitRecords(calculationCtx, records); err != nil {
		if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
			level.Debug(p.logger).Log("msg", "partition revoked, aborting index commit", "partition", record.Partition)
			return
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err, "partition", record.Partition)
		return
	}
}

// Appends a record and returns a slice of buffered events to index. The slice will be empty if no indexing is required.
func (p *Builder) appendRecord(record *kgo.Record) (context.Context, []bufferedEvent) {
	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return nil, nil
	}

	bufferedEvt := &bufferedEvent{
		event:  *event,
		record: record,
	}

	return p.bufferAndTryProcess(record.Partition, bufferedEvt, triggerTypeNormal)
}

func (p *Builder) cleanupPartition(partition int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	if p.cancelActiveCalculation != nil {
		p.cancelActiveCalculation(nil)
	}

	if state, ok := p.partitionStates[partition]; ok {
		// Clear processed events and reset processing flag
		state.events = state.events[:0]
		state.isProcessing = false
		state.lastActivity = time.Now()
	}
}

func (p *Builder) buildIndex(ctx context.Context, events []metastore.ObjectWrittenEvent) error {
	level.Debug(p.logger).Log("msg", "building index", "events", len(events), "partition", p.activeCalculationPartition)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to parse write time", "err", err)
		return err
	}
	p.metrics.setProcessingDelay(writeTime)

	// Trigger the downloads
	for _, event := range events {
		p.downloadQueue <- event
	}

	// Process the results as they are downloaded
	processingErrors := multierror.New()
	for i := 0; i < len(events); i++ {
		obj := <-p.downloadedObjects
		objLogger := log.With(p.logger, "object_path", obj.event.ObjectPath)
		level.Debug(objLogger).Log("msg", "processing object")

		if obj.err != nil {
			processingErrors.Add(fmt.Errorf("failed to download object: %w", obj.err))
			continue
		}

		reader, err := dataobj.FromReaderAt(bytes.NewReader(*obj.objectBytes), int64(len(*obj.objectBytes)))
		if err != nil {
			processingErrors.Add(fmt.Errorf("failed to read object: %w", err))
			continue
		}

		if err := p.calculator.Calculate(ctx, objLogger, reader, obj.event.ObjectPath); err != nil {
			processingErrors.Add(fmt.Errorf("failed to calculate index: %w", err))
			continue
		}
	}

	if processingErrors.Err() != nil {
		return processingErrors.Err()
	}

	tenantTimeRanges := p.calculator.TimeRanges()
	obj, closer, err := p.calculator.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}
	defer closer.Close()

	key, err := ObjectKey(ctx, obj)
	if err != nil {
		return fmt.Errorf("failed to generate object key: %w", err)
	}

	reader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("failed to read object: %w", err)
	}
	defer reader.Close()

	if err := p.indexStorageBucket.Upload(ctx, key, reader); err != nil {
		return fmt.Errorf("failed to upload index: %w", err)
	}

	metastoreTocWriter := metastore.NewTableOfContentsWriter(p.indexStorageBucket, p.logger)
	if err := metastoreTocWriter.WriteEntry(p.ctx, key, tenantTimeRanges); err != nil {
		return fmt.Errorf("failed to update metastore ToC file: %w", err)
	}

	level.Debug(p.logger).Log("msg", "finished building new index file", "partition", p.activeCalculationPartition, "events", len(events), "size", obj.Size(), "duration", time.Since(start), "tenants", len(tenantTimeRanges), "path", key)
	return nil
}

// ObjectKey determines the key in object storage to upload the object to, based on our path scheme.
func ObjectKey(ctx context.Context, object *dataobj.Object) (string, error) {
	h := sha256.New224()

	reader, err := object.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}

	var sumBytes [sha256.Size224]byte
	sum := h.Sum(sumBytes[:0])
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("indexes/%s/%s", sumStr[:2], sumStr[2:]), nil
}

func (p *Builder) checkAndFlushStalePartitions() {
	p.partitionsMutex.Lock()
	partitionsToFlush := make([]int32, 0)

	for partition, state := range p.partitionStates {
		if !state.isProcessing &&
			len(state.events) >= p.cfg.MinFlushEvents &&
			time.Since(state.lastActivity) >= p.cfg.MaxIdleTime {
			partitionsToFlush = append(partitionsToFlush, partition)
		}
	}
	p.partitionsMutex.Unlock()

	for _, partition := range partitionsToFlush {
		p.flushPartition(partition)
	}
}

func (p *Builder) flushPartition(partition int32) {
	ctx, eventsToFlush := p.bufferAndTryProcess(partition, nil, triggerTypeFlush)
	if len(eventsToFlush) == 0 {
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.cleanupPartition(partition)

		level.Info(p.logger).Log("msg", "flushing stale partition",
			"partition", partition, "events", len(eventsToFlush))

		// Build index
		events := make([]metastore.ObjectWrittenEvent, len(eventsToFlush))
		for i, buffered := range eventsToFlush {
			events[i] = buffered.event
		}

		if err := p.buildIndex(ctx, events); err != nil {
			if errors.Is(context.Cause(ctx), ErrPartitionRevoked) {
				level.Debug(p.logger).Log("msg", "partition revoked during flush", "partition", partition)
				return
			}
			level.Error(p.logger).Log("msg", "failed to flush partition", "partition", partition, "err", err)
			return
		}

		// Commit all records from the flush
		records := make([]*kgo.Record, len(eventsToFlush))
		for i, buffered := range eventsToFlush {
			records[i] = buffered.record
		}

		if err := p.commitRecords(ctx, records); err != nil {
			if errors.Is(context.Cause(ctx), ErrPartitionRevoked) {
				level.Debug(p.logger).Log("msg", "partition revoked during flush commit", "partition", partition)
				return
			}
			level.Error(p.logger).Log("msg", "failed to commit flush records", "partition", partition, "err", err)
		}
	}()
}

func (p *Builder) commitRecords(ctx context.Context, records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(ctx, records...)
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err, "count", len(records))
		p.metrics.incCommitFailures()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}
