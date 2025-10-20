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
	indexPath string
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

	// Download pipeline
	downloadQueue     chan metastore.ObjectWrittenEvent
	downloadedObjects chan downloadedObject

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
		downloadQueue:      make(chan metastore.ObjectWrittenEvent, 32),
		downloadedObjects:  make(chan downloadedObject, 1),
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

	// Start download worker
	si.downloadWorkerWg.Add(1)
	go si.downloadWorker(ctx)

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
	close(si.downloadQueue)
	close(si.buildRequestChan)

	// Wait for workers to finish
	si.downloadWorkerWg.Wait()
	si.buildWorkerWg.Wait()

	// Close the downloaded objects channel after workers are done
	close(si.downloadedObjects)

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

// downloadWorker handles object downloads
func (si *serialIndexer) downloadWorker(ctx context.Context) {
	defer si.downloadWorkerWg.Done()

	level.Debug(si.logger).Log("msg", "download worker started")
	defer level.Debug(si.logger).Log("msg", "download worker stopped")

	for {
		select {
		case event, ok := <-si.downloadQueue:
			if !ok {
				// Channel closed, worker should exit
				return
			}

			objLogger := log.With(si.logger, "object_path", event.ObjectPath)
			downloadStart := time.Now()

			objectReader, err := si.objectBucket.Get(ctx, event.ObjectPath)
			if err != nil {
				select {
				case si.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to fetch object from storage: %w", err),
				}:
				case <-ctx.Done():
					return
				}
				continue
			}

			object, err := io.ReadAll(objectReader)
			_ = objectReader.Close()
			if err != nil {
				select {
				case si.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to read object: %w", err),
				}:
				case <-ctx.Done():
					return
				}
				continue
			}

			level.Info(objLogger).Log("msg", "downloaded object", "duration", time.Since(downloadStart),
				"size_mb", float64(len(object))/1024/1024,
				"avg_speed_mbps", float64(len(object))/time.Since(downloadStart).Seconds()/1024/1024)

			select {
			case si.downloadedObjects <- downloadedObject{
				event:       event,
				objectBytes: &object,
			}:
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
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
	indexPath, err := si.buildIndex(req.ctx, events, req.partition)

	// Update metrics
	buildTime := time.Since(start)
	si.updateMetrics(buildTime)

	if err != nil {
		level.Error(si.logger).Log("msg", "failed to build index",
			"partition", req.partition, "err", err, "duration", buildTime)
		return buildResult{err: err}
	}

	level.Debug(si.logger).Log("msg", "successfully built index",
		"partition", req.partition, "index_path", indexPath, "duration", buildTime,
		"events", len(events))

	// Extract records for committing
	records := make([]*kgo.Record, len(req.events))
	for i, buffered := range req.events {
		records[i] = buffered.record
	}

	return buildResult{
		indexPath: indexPath,
		records:   records,
		err:       nil,
	}
}

// buildIndex is the core index building logic (moved from builder)
func (si *serialIndexer) buildIndex(ctx context.Context, events []metastore.ObjectWrittenEvent, partition int32) (string, error) {
	level.Debug(si.logger).Log("msg", "building index", "events", len(events), "partition", partition)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(si.logger).Log("msg", "failed to parse write time", "err", err)
		return "", err
	}
	si.builderMetrics.setProcessingDelay(writeTime)

	// Trigger the downloads
	for _, event := range events {
		select {
		case si.downloadQueue <- event:
			// Successfully sent event for download
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Process the results as they are downloaded
	processingErrors := multierror.New()
	for range len(events) {
		var obj downloadedObject
		select {
		case obj = <-si.downloadedObjects:
		case <-ctx.Done():
			return "", ctx.Err()
		}

		objLogger := log.With(si.logger, "object_path", obj.event.ObjectPath)
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

		if err := si.calculator.Calculate(ctx, objLogger, reader, obj.event.ObjectPath); err != nil {
			processingErrors.Add(fmt.Errorf("failed to calculate index: %w", err))
			continue
		}
	}

	if processingErrors.Err() != nil {
		return "", processingErrors.Err()
	}

	tenantTimeRanges := si.calculator.TimeRanges()
	obj, closer, err := si.calculator.Flush()
	if err != nil {
		return "", fmt.Errorf("failed to flush builder: %w", err)
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

	level.Debug(si.logger).Log("msg", "finished building new index file", "partition", partition,
		"events", len(events), "size", obj.Size(), "duration", time.Since(start),
		"tenants", len(tenantTimeRanges), "path", key)

	return key, nil
}

// updateMetrics updates internal build metrics
func (si *serialIndexer) updateMetrics(buildTime time.Duration) {
	si.indexerMetrics.incBuilds()
	si.indexerMetrics.setBuildTime(buildTime)
	si.indexerMetrics.setQueueDepth(len(si.buildRequestChan))
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
