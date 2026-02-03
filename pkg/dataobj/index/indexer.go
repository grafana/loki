package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// buildRequest represents a request to build an index
type buildRequest struct {
	events     []bufferedEvent
	partition  int32
	trigger    triggerType
	ctx        context.Context
	resultChan chan buildResult
}

// buildResult represents the result of an index build operation
type buildResult struct {
	indexPath string        // Index path when ErrBuilderFull causes multiple flushes
	records   []*kgo.Record // Records to commit after successful build
	err       error
}

// indexerConfig contains configuration for the indexer
type indexerConfig struct {
	QueueSize int // Size of the build request queue
}

// indexer handles serialized index building operations
type indexer interface {
	services.Service

	// submitBuild submits a build request and waits for completion
	submitBuild(ctx context.Context, events []bufferedEvent, partition int32, trigger triggerType) ([]*kgo.Record, error)
}

// serialIndexer implements Indexer with a single worker goroutine using dskit Service
type serialIndexer struct {
	services.Service

	// Dependencies - no builder dependency!
	calculator         calculator
	objectBucket       objstore.Bucket
	indexStorageBucket objstore.Bucket
	builderMetrics     *builderMetrics
	indexerMetrics     *indexerMetrics
	logger             log.Logger

	// Worker management
	buildRequestChan chan buildRequest
	buildWorkerWg    sync.WaitGroup
	downloadWorkerWg sync.WaitGroup
}

// newSerialIndexer creates a new self-contained SerialIndexer
func newSerialIndexer(
	calculator calculator,
	objectBucket objstore.Bucket,
	indexStorageBucket objstore.Bucket,
	builderMetrics *builderMetrics,
	indexerMetrics *indexerMetrics,
	logger log.Logger,
	cfg indexerConfig,
) *serialIndexer {
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 64
	}

	si := &serialIndexer{
		calculator:         calculator,
		objectBucket:       objectBucket,
		indexStorageBucket: indexStorageBucket,
		builderMetrics:     builderMetrics,
		indexerMetrics:     indexerMetrics,
		logger:             logger,
		buildRequestChan:   make(chan buildRequest, cfg.QueueSize),
	}

	// Initialize dskit Service
	si.Service = services.NewBasicService(si.starting, si.running, si.stopping)

	return si
}

// starting is called when the service is starting
func (si *serialIndexer) starting(_ context.Context) error {
	level.Info(si.logger).Log("msg", "starting serial indexer")
	return nil
}

// running is the main service loop
func (si *serialIndexer) running(ctx context.Context) error {
	level.Info(si.logger).Log("msg", "serial indexer running")

	// Start build worker
	si.buildWorkerWg.Add(1)
	go si.buildWorker(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// stopping is called when the service is stopping
func (si *serialIndexer) stopping(_ error) error {
	level.Info(si.logger).Log("msg", "stopping serial indexer")

	// Close channels to signal workers to stop
	close(si.buildRequestChan)

	// Wait for workers to finish
	si.downloadWorkerWg.Wait()
	si.buildWorkerWg.Wait()

	level.Info(si.logger).Log("msg", "stopped serial indexer")
	return nil
}

// submitBuild submits a build request and waits for completion
func (si *serialIndexer) submitBuild(ctx context.Context, events []bufferedEvent, partition int32, trigger triggerType) ([]*kgo.Record, error) {
	// Check if service is running
	if si.State() != services.Running {
		return nil, fmt.Errorf("indexer service is not running (state: %s)", si.State())
	}

	resultChan := make(chan buildResult, 1)

	req := buildRequest{
		events:     events,
		partition:  partition,
		trigger:    trigger,
		ctx:        ctx,
		resultChan: resultChan,
	}

	// Submit request
	select {
	case si.buildRequestChan <- req:
		si.indexerMetrics.incRequests()
		level.Debug(si.logger).Log("msg", "submitted build request",
			"partition", partition, "events", len(events), "trigger", trigger)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Wait for result
	select {
	case result := <-resultChan:
		if result.err != nil {
			level.Error(si.logger).Log("msg", "build request failed",
				"partition", partition, "err", result.err)
		} else {
			level.Debug(si.logger).Log("msg", "build request completed",
				"partition", partition, "index_path", result.indexPath)
		}
		return result.records, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// buildWorker is the main worker goroutine that processes build requests
func (si *serialIndexer) buildWorker(ctx context.Context) {
	defer si.buildWorkerWg.Done()

	level.Info(si.logger).Log("msg", "build worker started")
	defer level.Info(si.logger).Log("msg", "build worker stopped")

	for {
		select {
		case req, ok := <-si.buildRequestChan:
			if !ok {
				// Channel closed, worker should exit
				return
			}

			result := si.processBuildRequest(req)

			select {
			case req.resultChan <- result:
				// Result delivered successfully
			case <-req.ctx.Done():
				// Request was cancelled, but we already did the work
				level.Debug(si.logger).Log("msg", "build request was cancelled after completion",
					"partition", req.partition)
			case <-ctx.Done():
				// Service is shutting down
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// processBuildRequest processes a single build request - contains the full buildIndex logic
func (si *serialIndexer) processBuildRequest(req buildRequest) buildResult {
	start := time.Now()

	level.Debug(si.logger).Log("msg", "processing build request",
		"partition", req.partition, "events", len(req.events), "trigger", req.trigger)

	// Extract events for building
	events := make([]metastore.ObjectWrittenEvent, len(req.events))
	for i, buffered := range req.events {
		events[i] = buffered.event
	}

	// Build the index using internal method
	indexPath, processed, err := si.buildIndex(req.ctx, events, req.partition)

	// Update metrics
	buildTime := time.Since(start)
	si.updateMetrics(buildTime, getEarliestIndexedRecord(si.logger, events[:processed]))

	if err != nil {
		level.Error(si.logger).Log("msg", "failed to build index",
			"partition", req.partition, "err", err, "duration", buildTime, "processed", processed)
		return buildResult{err: err}
	}

	level.Debug(si.logger).Log("msg", "successfully built index",
		"partition", req.partition, "index_path", indexPath, "duration", buildTime,
		"events", len(events), "processed", processed)

	// Extract records for committing - only for processed events
	records := make([]*kgo.Record, processed)
	for i := range processed {
		records[i] = req.events[i].record
	}

	return buildResult{
		indexPath: indexPath,
		records:   records,
		err:       nil,
	}
}

func getEarliestIndexedRecord(logger log.Logger, events []metastore.ObjectWrittenEvent) time.Time {
	var earliestIndexedRecordTime time.Time
	for _, ev := range events {
		ts, err := time.Parse(time.RFC3339, ev.EarliestRecordTime)
		if err != nil {
			level.Error(logger).Log("msg", "failed to parse earliest record time", "err", err)
			continue
		}
		if ts.Before(earliestIndexedRecordTime) || earliestIndexedRecordTime.IsZero() {
			earliestIndexedRecordTime = ts
		}
	}
	return earliestIndexedRecordTime
}

// buildIndex is writing all metastore events to a single index object. It
// returns the index path and the number of events processed or an error if the index object is not created.
// The number of events processed can be less than the number of events if the builder becomes full
// when the trigger is triggerTypeAppend.
func (si *serialIndexer) buildIndex(ctx context.Context, events []metastore.ObjectWrittenEvent, partition int32) (string, int, error) {
	if len(events) == 0 {
		return "", 0, nil
	}

	level.Debug(si.logger).Log("msg", "building index", "events", len(events), "partition", partition)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(si.logger).Log("msg", "failed to parse write time", "err", err)
		return "", 0, err
	}
	si.builderMetrics.setProcessingDelay(writeTime)

	downloadQueue := make(chan metastore.ObjectWrittenEvent, 2)
	downloadedObjects := make(chan downloadedObject, 1)
	go downloadWorker(ctx, si.logger, downloadQueue, si.objectBucket, downloadedObjects)
	defer close(downloadQueue)

	// start downloading the first object
	downloadQueue <- events[0]
	nextEventIdx := 1

	// Process downloaded objects, handling ErrBuilderFull
	processingErrors := multierror.New()
	processed := 0
	for processed < len(events) {
		var obj downloadedObject
		select {
		case <-ctx.Done():
			return "", processed, ctx.Err()
		case obj = <-downloadedObjects:
		}

		// Start downloading the next object in the background while we're working on this one
		if nextEventIdx < len(events) {
			downloadQueue <- events[nextEventIdx]
			nextEventIdx++
		}

		processed++
		objLogger := log.With(si.logger, "object_path", obj.event.ObjectPath)
		level.Debug(objLogger).Log("msg", "processing object")

		if obj.err != nil {
			processingErrors.Add(fmt.Errorf("failed to download object: %w", obj.err))
			continue
		}

		// Process this object
		if err := si.processObject(ctx, objLogger, obj); err != nil {
			processingErrors.Add(fmt.Errorf("failed to process object: %w", err))
			continue
		}

		// Check if builder became full during processing
		if si.calculator.IsFull() {
			break
		}
	}

	if processingErrors.Err() != nil {
		return "", processed, processingErrors.Err()
	}

	// Flush current index and start fresh for each trigger type means:
	// - append: Either the calculator became full and the remaining events will be processed by the next trigger
	//           or all events have been processed, so we flush the current index and start fresh
	// - max-idle: All events have been processed, so we flush the current index and start fresh
	indexPath, flushErr := si.flushIndex(ctx, partition)
	if flushErr != nil {
		return "", processed, fmt.Errorf("failed to flush index after processing full object: %w", flushErr)
	}

	level.Debug(si.logger).Log("msg", "finished building index files", "partition", partition,
		"events", len(events), "processed", processed, "index_path", indexPath, "duration", time.Since(start))

	return indexPath, processed, nil
}

// downloadObject downloads an object from the bucket, attempting to pre-allocate
// the buffer based on object size from Attributes. Falls back to io.ReadAll if Attributes fails.
func downloadObject(ctx context.Context, bucket objstore.Bucket, path string) ([]byte, error) {
	reader, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch object from storage: %w", err)
	}
	defer reader.Close()

	// if possible, pre-allocate the buffer based on object size from Attributes
	attrs, err := bucket.Attributes(ctx, path)
	if err == nil && attrs.Size > 0 {
		buf := make([]byte, attrs.Size)
		_, err = io.ReadFull(reader, buf)
		if err != nil {
			return nil, fmt.Errorf("failed to read object: %w", err)
		}
		return buf, nil
	}

	// fallback to io.ReadAll if Attributes fails
	object, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read object: %w", err)
	}
	return object, nil
}

// downloadWorker processes downloads from the input downloadQueue and writes the resulting buffer to the downloadedObjects output channel.
// It exits when downloadQueue is closed.
func downloadWorker(ctx context.Context, logger log.Logger, downloadQueue chan metastore.ObjectWrittenEvent, objectBucket objstore.Bucket, downloadedObjects chan downloadedObject) {
	level.Debug(logger).Log("msg", "download worker started")
	defer level.Debug(logger).Log("msg", "download worker stopped")

	for event := range downloadQueue {
		objLogger := log.With(logger, "object_path", event.ObjectPath)
		downloadStart := time.Now()

		object, err := downloadObject(ctx, objectBucket, event.ObjectPath)
		if err != nil {
			downloadedObjects <- downloadedObject{
				event: event,
				err:   err,
			}
			continue
		}

		level.Info(objLogger).Log("msg", "downloaded object", "duration", time.Since(downloadStart),
			"size_mb", float64(len(object))/1024/1024,
			"avg_speed_mbps", float64(len(object))/time.Since(downloadStart).Seconds()/1024/1024)

		downloadedObjects <- downloadedObject{
			event:       event,
			objectBytes: &object,
		}
	}
	close(downloadedObjects)
}

// processObject handles processing a single downloaded object
func (si *serialIndexer) processObject(ctx context.Context, objLogger log.Logger, obj downloadedObject) error {
	reader, err := dataobj.FromReaderAt(bytes.NewReader(*obj.objectBytes), int64(len(*obj.objectBytes)))
	if err != nil {
		return fmt.Errorf("failed to read object: %w", err)
	}

	return si.calculator.Calculate(ctx, objLogger, reader, obj.event.ObjectPath)
}

// flushIndex flushes the current calculator state to an index object
func (si *serialIndexer) flushIndex(ctx context.Context, partition int32) (string, error) {
	tenantTimeRanges := si.calculator.TimeRanges()
	if len(tenantTimeRanges) == 0 {
		return "", nil // Nothing to flush
	}

	obj, closer, err := si.calculator.Flush()
	if err != nil {
		return "", fmt.Errorf("failed to flush calculator: %w", err)
	}
	defer closer.Close()

	key, err := ObjectKey(ctx, obj)
	if err != nil {
		return "", fmt.Errorf("failed to generate object key: %w", err)
	}

	reader, err := obj.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read object: %w", err)
	}
	defer reader.Close()

	if err := si.indexStorageBucket.Upload(ctx, key, reader); err != nil {
		return "", fmt.Errorf("failed to upload index: %w", err)
	}

	metastoreTocWriter := metastore.NewTableOfContentsWriter(si.indexStorageBucket, si.logger)
	if err := metastoreTocWriter.WriteEntry(ctx, key, tenantTimeRanges); err != nil {
		return "", fmt.Errorf("failed to update metastore ToC file: %w", err)
	}

	si.calculator.Reset()

	level.Debug(si.logger).Log("msg", "flushed index object", "partition", partition,
		"path", key, "size", obj.Size(), "tenants", len(tenantTimeRanges))

	return key, nil
}

// updateMetrics updates internal build metrics
func (si *serialIndexer) updateMetrics(buildTime time.Duration, earliestIndexedRecord time.Time) {
	si.indexerMetrics.incBuilds()
	si.indexerMetrics.setBuildTime(buildTime)
	si.indexerMetrics.setQueueDepth(len(si.buildRequestChan))
	if !earliestIndexedRecord.IsZero() {
		si.indexerMetrics.setEndToEndProcessingTime(time.Since(earliestIndexedRecord))
	}
}

// ObjectKey generates the object key for storing an index object in object storage.
// This is a public wrapper around the generateObjectKey functionality.
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
