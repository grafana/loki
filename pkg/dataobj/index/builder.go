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
	"strings"
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

type Config struct {
	indexobj.BuilderConfig `yaml:",inline"`
	EventsPerIndex         int `yaml:"events_per_index" experimental:"true"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.IntVar(&cfg.EventsPerIndex, prefix+"events-per-index", 32, "Experimental: The number of events to batch before building an index")
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

	bufferedEvents map[int32][]metastore.ObjectWrittenEvent

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
		bufferedEvents:     make(map[int32][]metastore.ObjectWrittenEvent),
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

func asString(partitions map[string][]int32) string {
	out := []string{}
	for topic, partitions := range partitions {
		out = append(out, fmt.Sprintf("%s: %v", topic, partitions))
	}
	return strings.Join(out, ", ")
}

func (p *Builder) handlePartitionsAssigned(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	level.Info(p.logger).Log("msg", "partitions assigned", "partitions", asString(topics))
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			p.bufferedEvents[partition] = make([]metastore.ObjectWrittenEvent, 0)
		}
	}
}

// This is not thread-safe
func (p *Builder) handlePartitionsRevoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	level.Info(p.logger).Log("msg", "partitions revoked", "partitions", asString(topics))
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			delete(p.bufferedEvents, partition)
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

	level.Info(p.logger).Log("msg", "started index builder service")
	for {
		fetches := p.client.PollRecords(ctx, -1)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			level.Error(p.logger).Log("msg", "error fetching records", "err", errs)
			continue
		}
		if fetches.Empty() {
			continue
		}
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			// TODO(benclive): Verify if we need to return re-poll ASAP or if sequential processing is good enough.
			for _, record := range ftp.Records {
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

func (p *Builder) processRecord(record *kgo.Record) {
	calculationCtx, eventsToIndex := p.appendRecord(record)
	if len(eventsToIndex) < p.cfg.EventsPerIndex {
		return
	}

	defer p.cleanupPartition(record.Partition)

	// Build the index.
	err := p.buildIndex(calculationCtx, eventsToIndex)
	if err != nil {
		if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
			level.Info(p.logger).Log("msg", "partition revoked, aborting index build", "partition", p.activeCalculationPartition)
			return
		}
		level.Error(p.logger).Log("msg", "failed to build index", "err", err, "partition", p.activeCalculationPartition)
		return
	}

	// Commit back to the partition we just built. This is always the record we just received, otherwise we would not have triggered the build.
	if err := p.commitRecords(calculationCtx, record); err != nil {
		if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
			level.Info(p.logger).Log("msg", "partition revoked, aborting index commit", "partition", p.activeCalculationPartition)
			return
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err, "partition", p.activeCalculationPartition)
		return
	}
}

// Appends a record and returns a slice of records to index. The slice will be empty if no indexing is required.
func (p *Builder) appendRecord(record *kgo.Record) (context.Context, []metastore.ObjectWrittenEvent) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return nil, nil
	}

	_, ok := p.bufferedEvents[record.Partition]
	if !ok {
		// We don't own this partition anymore as it was just revoked. Abort further processing.
		return nil, nil
	}

	p.bufferedEvents[record.Partition] = append(p.bufferedEvents[record.Partition], *event)
	level.Info(p.logger).Log("msg", "buffered new event for partition", "count", len(p.bufferedEvents[record.Partition]), "partition", record.Partition)

	if len(p.bufferedEvents[record.Partition]) < p.cfg.EventsPerIndex {
		// No more work to do
		return nil, nil
	}

	var calculationCtx context.Context
	eventsToIndex := make([]metastore.ObjectWrittenEvent, len(p.bufferedEvents[record.Partition]))
	copy(eventsToIndex, p.bufferedEvents[record.Partition])

	p.activeCalculationPartition = record.Partition
	calculationCtx, p.cancelActiveCalculation = context.WithCancelCause(p.ctx)

	return calculationCtx, eventsToIndex
}

func (p *Builder) cleanupPartition(partition int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	p.cancelActiveCalculation(nil)

	if _, ok := p.bufferedEvents[partition]; !ok {
		// We don't own this partition anymore as it was just revoked. The revocation process has already cleaned up any map entries.
		return
	}
	// We still own this partition, just truncate the events for future processing.
	p.bufferedEvents[partition] = p.bufferedEvents[partition][:0]
}

func (p *Builder) buildIndex(ctx context.Context, events []metastore.ObjectWrittenEvent) error {
	level.Info(p.logger).Log("msg", "building index", "events", len(events), "partition", p.activeCalculationPartition)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to parse write time", "err", err)
		return err
	}
	p.metrics.observeProcessingDelay(writeTime)

	// Trigger the downloads
	for _, event := range events {
		p.downloadQueue <- event
	}

	// Process the results as they are downloaded
	processingErrors := multierror.New()
	for i := 0; i < len(events); i++ {
		obj := <-p.downloadedObjects
		objLogger := log.With(p.logger, "object_path", obj.event.ObjectPath)
		level.Info(objLogger).Log("msg", "processing object")

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

	level.Info(p.logger).Log("msg", "finished building new index file", "partition", p.activeCalculationPartition, "events", len(events), "size", obj.Size(), "duration", time.Since(start), "tenants", len(tenantTimeRanges), "path", key)
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

	return fmt.Sprintf("multi-tenant/indexes/%s/%s", sumStr[:2], sumStr[2:]), nil
}

func (p *Builder) commitRecords(ctx context.Context, record *kgo.Record) error {
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(ctx, record)
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
		p.metrics.incCommitFailures()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}
