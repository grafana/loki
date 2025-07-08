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
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"github.com/bits-and-blooms/bloom/v3"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type Config struct {
	indexobj.BuilderConfig `yaml:",inline"`
	EventsPerIndex         int                    `yaml:"events_per_index" experimental:"true"`
	IndexStoragePrefix     string                 `yaml:"index_storage_prefix" experimental:"true"`
	EnabledTenantIDs       flagext.StringSliceCSV `yaml:"enabled_tenant_ids" experimental:"true"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.IntVar(&cfg.EventsPerIndex, prefix+"events-per-index", 32, "Experimental: The number of events to batch before building an index")
	f.StringVar(&cfg.IndexStoragePrefix, prefix+"storage-prefix", "index/v0/", "Experimental: A prefix to use for storing indexes in object storage. Used to separate the metastore & index files during initial testing.")
	f.Var(&cfg.EnabledTenantIDs, prefix+"enabled-tenant-ids", "Experimental: A list of tenant IDs to enable index building for. If empty, all tenants will be enabled.")
}

type downloadedObject struct {
	event       metastore.ObjectWrittenEvent
	objectBytes *[]byte
	err         error
}

const indexEventTopic = "loki.metastore-events"
const indexConsumerGroup = "metastore-event-reader"

type Builder struct {
	services.Service

	cfg Config

	// Kafka client and topic/partition info
	client *kgo.Client
	topic  string

	// Processing pipeline
	downloadQueue     chan metastore.ObjectWrittenEvent
	downloadedObjects chan downloadedObject
	builder           *indexobj.Builder

	bufferedEvents map[string][]metastore.ObjectWrittenEvent

	// Builder initialization
	builderCfg  indexobj.BuilderConfig
	bucket      objstore.Bucket
	flushBuffer *bytes.Buffer

	// Metrics
	metrics *indexBuilderMetrics

	// Control and coordination
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	logger     log.Logger
	builderMtx sync.Mutex
}

func NewIndexBuilder(
	cfg Config,
	kafkaCfg kafka.Config,
	logger log.Logger,
	instanceID string,
	bucket objstore.Bucket,
	reg prometheus.Registerer,
) (*Builder, error) {
	kafkaCfg.AutoCreateTopicEnabled = true
	kafkaCfg.AutoCreateTopicDefaultPartitions = 64
	eventConsumerClient, err := client.NewReaderClient(
		"index_builder",
		kafkaCfg,
		logger,
		reg,
		kgo.ConsumeTopics(indexEventTopic),
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.ConsumerGroup(indexConsumerGroup),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer client: %w", err)
	}

	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     indexEventTopic,
		"component": "index_builder",
	}, reg)

	metrics := newIndexBuilderMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	builder, err := indexobj.NewBuilder(cfg.BuilderConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	if err := builder.RegisterMetrics(reg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for builder: %w", err)
	}

	// Allocate a single buffer
	flushBuffer := bytes.NewBuffer(make([]byte, int(float64(cfg.BuilderConfig.TargetObjectSize)*1.2)))

	// Set up queues to download the next object (I/O bound) while processing the current one (CPU bound) in order to maximize throughput.
	// Setting the channel buffer sizes caps the total memory usage by only keeping up to 3 objects in memory at a time: One being processed, one fully downloaded and one being downloaded from the queue.
	downloadQueue := make(chan metastore.ObjectWrittenEvent, cfg.EventsPerIndex)
	downloadedObjects := make(chan downloadedObject, 1)

	s := &Builder{
		cfg:               cfg,
		client:            eventConsumerClient,
		logger:            logger,
		builder:           builder,
		bucket:            bucket,
		flushBuffer:       flushBuffer,
		builderMtx:        sync.Mutex{},
		downloadedObjects: downloadedObjects,
		downloadQueue:     downloadQueue,
		metrics:           metrics,

		bufferedEvents: make(map[string][]metastore.ObjectWrittenEvent),
	}

	s.Service = services.NewBasicService(nil, s.run, s.stopping)

	return s, nil
}

func (p *Builder) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	p.ctx = ctx
	p.cancel = cancel

	p.wg.Add(1)
	go func() {
		// Download worker
		defer p.wg.Done()
		for event := range p.downloadQueue {
			objLogger := log.With(p.logger, "object_path", event.ObjectPath)
			downloadStart := time.Now()

			objectReader, err := p.bucket.Get(p.ctx, event.ObjectPath)
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
	p.cancel()
	p.wg.Wait()
	close(p.downloadedObjects)
	p.client.Close()
	return failureCase
}

func (p *Builder) processRecord(record *kgo.Record) {
	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return
	}
	p.bufferedEvents[event.Tenant] = append(p.bufferedEvents[event.Tenant], *event)
	level.Info(p.logger).Log("msg", "buffered new event for tenant", "count", len(p.bufferedEvents[event.Tenant]), "tenant", event.Tenant)

	if len(p.bufferedEvents[event.Tenant]) >= p.cfg.EventsPerIndex {
		if !slices.Contains(p.cfg.EnabledTenantIDs, event.Tenant) {
			// TODO(benclive): Remove this check once builders handle multi-tenancy when building indexes.
			level.Info(p.logger).Log("msg", "skipping index build for disabled tenant", "tenant", event.Tenant)
			p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
			return
		}
		err := p.buildIndex(p.bufferedEvents[event.Tenant][:len(p.bufferedEvents[event.Tenant])])
		if err != nil {
			// TODO(benclive): Improve error handling for failed index builds.
			panic(err)
		}

		if err := p.commitRecords(record); err != nil {
			level.Warn(p.logger).Log("msg", "failed to commit records", "err", err)
			return
		}
		p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
	}
}

func (p *Builder) buildIndex(events []metastore.ObjectWrittenEvent) error {
	indexStorageBucket := objstore.NewPrefixedBucket(p.bucket, p.cfg.IndexStoragePrefix)
	level.Info(p.logger).Log("msg", "building index", "events", len(events), "tenant", events[0].Tenant)
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
		level.Info(objLogger).Log("msg", "processing object", "size", len(*obj.objectBytes))

		if obj.err != nil {
			processingErrors.Add(fmt.Errorf("failed to download object: %w", obj.err))
			continue
		}

		reader, err := dataobj.FromReaderAt(bytes.NewReader(*obj.objectBytes), int64(len(*obj.objectBytes)))
		if err != nil {
			processingErrors.Add(fmt.Errorf("failed to read object: %w", err))
			continue
		}

		// Streams Section: process this section first to ensure all streams have been added to the builder and are given new IDs.
		for i, section := range reader.Sections().Filter(streams.CheckSection) {
			level.Debug(objLogger).Log("msg", "processing streams section", "index", i)
			if err := p.processStreamsSection(section, obj.event.ObjectPath); err != nil {
				processingErrors.Add(fmt.Errorf("failed to process stream section: %w", err))
				continue
			}
		}

		// Logs Section: these can be processed in parallel once we have the stream IDs.
		g, ctx := errgroup.WithContext(p.ctx)
		for i, section := range reader.Sections().Filter(logs.CheckSection) {
			g.Go(func() error {
				sectionLogger := log.With(objLogger, "section", i)
				level.Debug(sectionLogger).Log("msg", "processing logs section")
				// 1. A bloom filter for each column in the logs section.
				// 2. A per-section stream time-range index using min/max of each stream in the logs section. StreamIDs will reference the aggregate stream section.
				if err := p.processLogsSection(ctx, sectionLogger, obj.event.ObjectPath, section, int64(i)); err != nil {
					return fmt.Errorf("failed to process logs section path=%s section=%d: %w", obj.event.ObjectPath, i, err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			processingErrors.Add(fmt.Errorf("failed to process logs sections: %w", err))
			continue
		}
	}

	if processingErrors.Err() != nil {
		return processingErrors.Err()
	}

	p.flushBuffer.Reset()
	stats, err := p.builder.Flush(p.flushBuffer)
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}

	size := p.flushBuffer.Len()

	key := p.getKey(events[0].Tenant, p.flushBuffer)
	if err := indexStorageBucket.Upload(p.ctx, key, p.flushBuffer); err != nil {
		return fmt.Errorf("failed to upload index: %w", err)
	}

	metastoreUpdater := metastore.NewUpdater(indexStorageBucket, events[0].Tenant, p.logger)
	if stats.MinTimestamp.IsZero() || stats.MaxTimestamp.IsZero() {
		return errors.New("failed to get min/max timestamps")
	}
	if err := metastoreUpdater.Update(p.ctx, key, stats.MinTimestamp, stats.MaxTimestamp); err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	level.Info(p.logger).Log("msg", "finished building index", "tenant", events[0].Tenant, "events", len(events), "size", size, "duration", time.Since(start))
	return nil
}

// getKey determines the key in object storage to upload the object to, based on our path scheme.
func (p *Builder) getKey(tenantID string, object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("tenant-%s/indexes/%s/%s", tenantID, sumStr[:2], sumStr[2:])
}

func (p *Builder) processStreamsSection(section *dataobj.Section, objectPath string) error {
	streamSection, err := streams.Open(p.ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open stream section: %w", err)
	}

	streamBuf := make([]streams.Stream, 2048)
	rowReader := streams.NewRowReader(streamSection)
	cnt := 0
	for {
		n, err := rowReader.Read(p.ctx, streamBuf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read stream section: %w", err)
		}
		if n == 0 && err == io.EOF {
			break
		}
		cnt += n
		for _, stream := range streamBuf[:n] {
			newStreamID, err := p.builder.AppendStream(stream)
			if err != nil {
				return fmt.Errorf("failed to append to stream: %w", err)
			}
			p.builder.RecordStreamRef(objectPath, stream.ID, newStreamID)
		}
	}
	fmt.Printf("streams section: %d rows\n", cnt)
	return nil
}

// processLogsSection reads information from the logs section in order to build index information in a new object.
func (p *Builder) processLogsSection(ctx context.Context, sectionLogger log.Logger, objectPath string, section *dataobj.Section, sectionIdx int64) error {
	logsBuf := make([]logs.Record, 1024)
	type logInfo struct {
		objectPath string
		sectionIdx int64
		streamID   int64
		timestamp  time.Time
		length     int64
	}
	logsInfo := make([]logInfo, len(logsBuf))

	logsSection, err := logs.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open logs section: %w", err)
	}

	// Fetch the column statistics in order to init the bloom filters for each column
	stats, err := logs.ReadStats(ctx, logsSection)
	if err != nil {
		return fmt.Errorf("failed to read log section stats: %w", err)
	}

	columnBloomBuilders := make(map[string]*bloom.BloomFilter)
	columnIndexes := make(map[string]int64)
	for _, column := range stats.Columns {
		if !logs.IsMetadataColumn(column.Type) {
			continue
		}
		columnBloomBuilders[column.Name] = bloom.NewWithEstimates(uint(column.Cardinality), 1.0/128.0)
		columnIndexes[column.Name] = column.ColumnIndex
	}

	// Read the whole logs section to extract all the column values.
	cnt := 0
	// TODO(benclive): Switch to a columnar reader instead of row based
	// This is also likely to be more performant, especially if we don't need to read the whole log line.
	// Note: the source object would need a new column storing just the length to avoid reading the log line itself.
	rowReader := logs.NewRowReader(logsSection)
	defer rowReader.Close()
	level.Info(sectionLogger).Log("msg", "initialized row reader", "count", len(columnBloomBuilders), "rowReader", rowReader)
	for {
		n, err := rowReader.Read(ctx, logsBuf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read logs section: %w", err)
		}
		if n == 0 && err == io.EOF {
			level.Warn(sectionLogger).Log("msg", "EOF while reading logs section", "rowsProcessed", cnt)
			break
		}

		cnt += n
		for i, log := range logsBuf[:n] {
			for _, md := range log.Metadata {
				columnBloomBuilders[md.Name].Add([]byte(md.Value))
			}
			logsInfo[i].objectPath = objectPath
			logsInfo[i].sectionIdx = sectionIdx
			logsInfo[i].streamID = log.StreamID
			logsInfo[i].timestamp = log.Timestamp
			logsInfo[i].length = int64(len(log.Line))
		}

		// Lock the mutex once per read for perf reasons.
		p.builderMtx.Lock()
		for _, log := range logsInfo[:n] {
			err = p.builder.ObserveLogLine(log.objectPath, log.sectionIdx, log.streamID, log.timestamp, log.length)
			if err != nil {
				p.builderMtx.Unlock()
				return fmt.Errorf("failed to observe log line: %w", err)
			}
		}
		p.builderMtx.Unlock()
	}

	// Write the indexes (bloom filters) to the new index object.
	for columnName, bloom := range columnBloomBuilders {
		bloomBytes, err := bloom.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal bloom filter: %w", err)
		}
		p.builderMtx.Lock()
		err = p.builder.AppendColumnIndex(objectPath, sectionIdx, columnName, columnIndexes[columnName], bloomBytes)
		p.builderMtx.Unlock()
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}
	}

	level.Info(sectionLogger).Log("msg", "finished processing logs section", "rowsProcessed", cnt)
	return nil
}

func (p *Builder) commitRecords(record *kgo.Record) error {
	backoff := backoff.New(p.ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(p.ctx, record)
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
