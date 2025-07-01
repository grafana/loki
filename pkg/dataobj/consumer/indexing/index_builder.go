package indexing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/indexing/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type downloadedObject struct {
	event       metastore.ObjectWrittenEvent
	objectBytes *[]byte
}

type IndexBuilder struct {
	// Kafka client and topic/partition info
	client *kgo.Client
	topic  string

	// Processing pipeline
	records           chan *kgo.Record
	downloadQueue     chan metastore.ObjectWrittenEvent
	downloadedObjects chan downloadedObject
	builder           *indexobj.Builder

	// Config params
	eventsPerIndex     int
	indexStoragePrefix string
	enabledTenantIDs   []string

	bufferedEvents map[string][]metastore.ObjectWrittenEvent

	// Builder initialization
	builderCfg  indexobj.BuilderConfig
	bucket      objstore.Bucket
	flushBuffer *bytes.Buffer

	// Metrics
	metrics *indexBuilderMetrics

	// Control and coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	reg    prometheus.Registerer
	logger log.Logger
	mtx    sync.Mutex
}

func NewIndexBuilder(
	ctx context.Context,
	logger log.Logger,
	client *kgo.Client,
	builderCfg indexobj.BuilderConfig,
	bucket objstore.Bucket,
	topic string,
	reg prometheus.Registerer,
	eventsPerIndex int,
	indexStoragePrefix string,
	enabledTenantIDs []string,
) *IndexBuilder {
	ctx, cancel := context.WithCancel(ctx)
	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     topic,
		"component": "index_processor",
	}, reg)

	metrics := newIndexBuilderMetrics()

	builder, err := indexobj.NewBuilder(builderCfg)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create builder", "err", err)
	}

	if err := builder.RegisterMetrics(reg); err != nil {
		level.Error(logger).Log("msg", "failed to register metrics for builder", "err", err)
	}

	flushBuffer := bytes.NewBuffer(make([]byte, int(float64(builderCfg.TargetObjectSize)*1.2)))

	// Set up queues to download the next object (I/O bound) while processing the current one (CPU bound) in order to maximize throughput.
	// Setting the channel buffer sizes caps the total memory usage by only keeping up to 3 objects in memory at a time: One being processed, one fully downloaded and one being downloaded from the queue.
	downloadQueue := make(chan metastore.ObjectWrittenEvent, 32)
	downloadedObjects := make(chan downloadedObject, 1)

	return &IndexBuilder{
		client:             client,
		logger:             log.With(logger, "topic", topic),
		topic:              topic,
		records:            make(chan *kgo.Record, 100),
		ctx:                ctx,
		cancel:             cancel,
		reg:                reg,
		builderCfg:         builderCfg,
		builder:            builder,
		bucket:             bucket,
		flushBuffer:        flushBuffer,
		mtx:                sync.Mutex{},
		downloadedObjects:  downloadedObjects,
		downloadQueue:      downloadQueue,
		metrics:            metrics,
		eventsPerIndex:     eventsPerIndex,
		indexStoragePrefix: indexStoragePrefix,
		enabledTenantIDs:   enabledTenantIDs,

		bufferedEvents: make(map[string][]metastore.ObjectWrittenEvent),
	}
}

func (p *IndexBuilder) Start() {
	if err := p.metrics.register(p.reg); err != nil {
		level.Error(p.logger).Log("msg", "failed to register metrics for index builder", "err", err)
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		level.Info(p.logger).Log("msg", "started index processor")
		for {
			select {
			case <-p.ctx.Done():
				level.Info(p.logger).Log("msg", "stopping index processor")
				return
			case record, ok := <-p.records:
				if !ok {
					// Channel was closed
					return
				}
				p.processRecord(record)
			}
		}
	}()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for event := range p.downloadQueue {
			objLogger := log.With(p.logger, "object_path", event.ObjectPath)
			downloadStart := time.Now()

			objectReader, err := p.bucket.Get(p.ctx, event.ObjectPath)
			if err != nil {
				level.Error(objLogger).Log("msg", "failed to read object", "err", err)
				continue
			}

			object, err := io.ReadAll(objectReader)
			_ = objectReader.Close()
			if err != nil {
				level.Error(objLogger).Log("msg", "failed to read object", "err", err)
				continue
			}
			level.Info(objLogger).Log("msg", "downloaded object", "duration", time.Since(downloadStart), "size_mb", float64(len(object))/1024/1024, "avg_speed_mbps", float64(len(object))/time.Since(downloadStart).Seconds()/1024/1024)
			p.downloadedObjects <- downloadedObject{
				event:       event,
				objectBytes: &object,
			}
		}
	}()
}

func (p *IndexBuilder) Stop() {
	close(p.downloadQueue)
	p.cancel()
	p.wg.Wait()
	close(p.downloadedObjects)
	if p.builder != nil {
		p.builder.UnregisterMetrics(p.reg)
	}
	p.metrics.unregister(p.reg)
}

// Drops records from the channel if the processor is stopped.
// Returns false if the processor is stopped, true otherwise.
func (p *IndexBuilder) Append(records []*kgo.Record) bool {
	for _, record := range records {
		select {
		// must check per-record in order to not block on a full channel
		// after receiver has been stopped.
		case <-p.ctx.Done():
			return false
		case p.records <- record:
		}
	}
	return true
}

func (p *IndexBuilder) processRecord(record *kgo.Record) {
	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return
	}
	p.bufferedEvents[event.Tenant] = append(p.bufferedEvents[event.Tenant], *event)
	level.Info(p.logger).Log("msg", "buffered new event for tenant", "count", len(p.bufferedEvents[event.Tenant]), "tenant", event.Tenant)

	if len(p.bufferedEvents[event.Tenant]) >= p.eventsPerIndex {
		if !slices.Contains(p.enabledTenantIDs, event.Tenant) {
			// TODO(benclive): Remove this check once builders handle multi-tenancy when building indexes.
			level.Info(p.logger).Log("msg", "skipping index build for disabled tenant", "tenant", event.Tenant)
			p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
			return
		}
		err := p.buildIndex(p.bufferedEvents[event.Tenant][:len(p.bufferedEvents[event.Tenant])])
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to build index", "err", err)
			panic(err)
		}

		if err := p.commitRecords(record); err != nil {
			level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
			return
		}
		p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
	}
}

func (p *IndexBuilder) buildIndex(events []metastore.ObjectWrittenEvent) error {
	indexStorageBucket := objstore.NewPrefixedBucket(p.bucket, p.indexStoragePrefix)
	level.Info(p.logger).Log("msg", "building index", "events", len(events), "tenant", events[0].Tenant)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to parse write time", "err", err)
		return err
	}
	p.metrics.observeProcessingDelay(writeTime)

	p.metrics.incAppendsTotal()

	// Trigger the downloads
	for _, event := range events {
		p.downloadQueue <- event
	}

	// Process the results as they are downloaded
	eventsProcessed := 0
	for obj := range p.downloadedObjects {
		objLogger := log.With(p.logger, "object_path", obj.event.ObjectPath)
		level.Info(objLogger).Log("msg", "indexing object", "object_path", obj.event.ObjectPath)
		reader, err := dataobj.FromReaderAt(bytes.NewReader(*obj.objectBytes), int64(len(*obj.objectBytes)))
		if err != nil {
			level.Error(objLogger).Log("msg", "failed to read object", "err", err)
			return err
		}

		// Streams Section: process this first to ensure all streams have been added to the builder and are given new IDs.
		for i, section := range reader.Sections() {
			if !streams.CheckSection(section) {
				continue
			}
			level.Info(objLogger).Log("msg", "processing streams section", "index", i)
			if err := p.processStreamsSection(objLogger, section, obj.event.ObjectPath); err != nil {
				level.Error(objLogger).Log("msg", "failed to handle stream section", "err", err)
				return err
			}
		}

		g, ctx := errgroup.WithContext(p.ctx)
		// Logs Section: these can be processed in parallel once we have the stream IDs.
		for i, section := range reader.Sections() {
			if !logs.CheckSection(section) {
				continue
			}
			g.Go(func() error {
				level.Info(objLogger).Log("msg", "processing logs section", "index", i)
				// 1. A bloom filter for each column in the logs section.
				// 2. A per-section stream time-range index using min/max of each stream in the logs section. StreamIDs will reference the aggregate stream section.
				if err := p.processLogsSection(ctx, objLogger, obj.event.ObjectPath, section, int64(i)); err != nil {
					level.Error(objLogger).Log("msg", "failed to handle logs section", "err", err)
					return err
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			level.Error(objLogger).Log("msg", "failed to process logs sections", "err", err)
			return err
		}
		eventsProcessed++
		if eventsProcessed == len(events) {
			level.Info(objLogger).Log("msg", "all events processed", "events", len(events), "duration", time.Since(start))
			break
		}
	}

	p.flushBuffer.Reset()
	stats, err := p.builder.Flush(p.flushBuffer)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to flush builder", "err", err)
		return err
	}

	size := p.flushBuffer.Len()

	key := p.getKey(events[0].Tenant, p.flushBuffer)
	if err := indexStorageBucket.Upload(p.ctx, key, p.flushBuffer); err != nil {
		level.Error(p.logger).Log("msg", "failed to upload index", "err", err)
		return err
	}

	metastoreUpdater := metastore.NewUpdater(indexStorageBucket, events[0].Tenant, p.logger)
	if stats.MinTimestamp.IsZero() || stats.MaxTimestamp.IsZero() {
		level.Error(p.logger).Log("msg", "failed to get min/max timestamps", "min", stats.MinTimestamp, "max", stats.MaxTimestamp)
		return errors.New("failed to get min/max timestamps")
	}
	if err := metastoreUpdater.Update(p.ctx, key, stats.MinTimestamp, stats.MaxTimestamp); err != nil {
		level.Error(p.logger).Log("msg", "failed to update metastore", "err", err)
		return err
	}

	level.Info(p.logger).Log("msg", "index built", "tenant", events[0].Tenant, "events", len(events), "size", size, "duration", time.Since(start))
	return nil
}

// getKey determines the key in object storage to upload the object to, based on our path scheme.
func (p *IndexBuilder) getKey(tenantID string, object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("tenant-%s/indexes/%s/%s", tenantID, sumStr[:2], sumStr[2:])
}

func (p *IndexBuilder) processStreamsSection(objLogger log.Logger, section *dataobj.Section, objectPath string) /*map[int64]streams.Stream, map[uint64]int64,*/ error {
	streamSection, err := streams.Open(p.ctx, section)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to open stream", "err", err)
		return err
	}

	streamBuf := make([]streams.Stream, 2048)
	rowReader := streams.NewRowReader(streamSection)
	for {
		n, err := rowReader.Read(p.ctx, streamBuf)
		if err != nil && err != io.EOF {
			level.Error(objLogger).Log("msg", "failed to read stream", "err", err)
			return err
		}
		if n == 0 && err == io.EOF {
			break
		}
		for _, stream := range streamBuf[:n] {
			newStreamID, err := p.builder.AppendStream(stream)
			if err != nil {
				level.Error(objLogger).Log("msg", "failed to append stream", "err", err)
				return err
			}
			p.mtx.Lock()
			p.builder.RecordStreamRef(objectPath, stream.ID, newStreamID)
			p.mtx.Unlock()
		}
	}
	return nil
}

// processLogsSection reads information from the logs section in order to build index information in a new object.
func (p *IndexBuilder) processLogsSection(ctx context.Context, objLogger log.Logger, objectPath string, section *dataobj.Section, sectionIdx int64) error {
	sectionLogger := log.With(objLogger, "section", sectionIdx)
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
		level.Error(p.logger).Log("msg", "failed to open logs", "err", err)
		return err
	}

	// Fetch the column statistics in order to init the bloom filters for each column
	stats, err := logs.ReadStats(ctx, logsSection)
	if err != nil {
		level.Error(objLogger).Log("msg", "failed to read logs stats", "err", err)
		return err
	}

	columnBloomBuilders := make(map[string]*bloom.BloomFilter)
	columnIndexes := make(map[string]int64)
	for _, column := range stats.Columns {
		if column.Type != logs.ColumnTypeMetadata.String() {
			continue
		}
		columnBloomBuilders[column.Name] = bloom.NewWithEstimates(uint(column.Cardinality), 1.0/128.0)
		columnIndexes[column.Name] = column.ColumnIndex
	}

	// Read the whole logs section to extract all the column values.
	cnt := 0
	rowReader := logs.NewRowReader(logsSection)
	for {
		n, err := rowReader.Read(p.ctx, logsBuf)
		if err != nil && err != io.EOF {
			level.Error(objLogger).Log("msg", "failed to read logs", "err", err)
			return err
		}
		if n == 0 && err == io.EOF {
			break
		}

		// TODO(benclive): Consider doing this column by column to avoid reading whole rows when we don't care about some of the columns (i.e. the content of the log line)
		// Note: the object would need a new column storing just the line length to avoid reading the log line itself, here.
		for i, log := range logsBuf[:n] {
			cnt++
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
		p.mtx.Lock()
		for _, log := range logsInfo[:n] {
			err = p.builder.ObserveLogLine(log.objectPath, log.sectionIdx, log.streamID, log.timestamp, log.length)
			if err != nil {
				level.Error(objLogger).Log("msg", "failed to observe stream", "err", err)
				return err
			}
		}
		p.mtx.Unlock()
	}

	// Write the indexes (bloom filters) to the new index object.
	for columnName, bloom := range columnBloomBuilders {
		bloomBytes, err := bloom.MarshalBinary()
		if err != nil {
			level.Error(objLogger).Log("msg", "failed to marshal bloom filter", "err", err)
			return err
		}
		p.mtx.Lock()
		err = p.builder.AppendColumnIndex(objectPath, sectionIdx, columnName, columnIndexes[columnName], bloomBytes)
		p.mtx.Unlock()
		if err != nil {
			level.Error(objLogger).Log("msg", "failed to append column index", "err", err)
			return err
		}
	}

	level.Info(sectionLogger).Log("msg", "finished processing logs section", "rowsProcessed", cnt)
	return nil
}

func (p *IndexBuilder) commitRecords(record *kgo.Record) error {
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
