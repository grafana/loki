package deletion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	maxChunksPerJob              = 1000
	storageUpdatesFilenameSuffix = `-storage-updates.json`
)

type StorageUpdatesIterator interface {
	Next() bool
	UserID() string
	TableName() string
	Err() error
	ForEachSeries(callback func(labels string, chunksToDelete []string, chunksToDeIndex []string, chunksToIndex []Chunk) error) error
}

type deletionJob struct {
	TableName      string          `json:"table_name"`
	UserID         string          `json:"user_id"`
	ChunkIDs       []string        `json:"chunk_ids"`
	DeleteRequests []DeleteRequest `json:"delete_requests"`
}

type jobDetails struct {
	labels string
}

type manifestJobs struct {
	jobsInProgress map[string]jobDetails
	cancel         context.CancelFunc
	manifestPath   string
}

type ApplyStorageUpdatesFunc func(ctx context.Context, iterator StorageUpdatesIterator) error
type markRequestsAsProcessedFunc func(requests []DeleteRequest) error

type JobBuilder struct {
	deleteStoreClient           client.ObjectClient
	applyStorageUpdatesFunc     ApplyStorageUpdatesFunc
	markRequestsAsProcessedFunc markRequestsAsProcessedFunc

	// Current manifest being processed
	currentManifest    manifestJobs
	currentManifestMtx sync.RWMutex

	currSegmentStorageUpdates *storageUpdatesCollection
}

func NewJobBuilder(deleteStoreClient client.ObjectClient, applyStorageUpdatesFunc ApplyStorageUpdatesFunc, markRequestsAsProcessedFunc markRequestsAsProcessedFunc) *JobBuilder {
	return &JobBuilder{
		deleteStoreClient:           deleteStoreClient,
		applyStorageUpdatesFunc:     applyStorageUpdatesFunc,
		markRequestsAsProcessedFunc: markRequestsAsProcessedFunc,
		currSegmentStorageUpdates: &storageUpdatesCollection{
			StorageUpdates: map[string]*storageUpdates{},
		},
	}
}

// BuildJobs implements jobqueue.Builder interface
func (b *JobBuilder) BuildJobs(ctx context.Context, jobsChan chan<- *grpc.Job) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		if err := b.buildJobs(ctx, jobsChan); err != nil {
			// ToDo(Sandeep): Add a metric for tracking failures in building jobs
			level.Error(util_log.Logger).Log("msg", "error building jobs", "err", err)
		}

		// Wait for next tick or context cancellation
		select {
		case <-ctx.Done():
		case <-ticker.C:
			// Continue to next iteration
		}
	}
}

func (b *JobBuilder) buildJobs(ctx context.Context, jobsChan chan<- *grpc.Job) error {
	// List all manifest directories
	manifests, err := b.listManifests(ctx)
	if err != nil {
		return err
	}

	// Process each manifest
	for _, manifestPath := range manifests {
		manifest, err := b.readManifest(ctx, manifestPath)
		if err != nil {
			return err
		}

		if err := b.processManifest(ctx, manifest, manifestPath, jobsChan); err != nil {
			return err
		}

		if err := b.applyStorageUpdates(ctx, manifest, manifestPath); err != nil {
			return err
		}

		if err := b.cleanupManifest(ctx, manifest, manifestPath); err != nil {
			return err
		}
	}

	return nil
}

func (b *JobBuilder) processManifest(ctx context.Context, manifest *manifest, manifestPath string, jobsChan chan<- *grpc.Job) error {
	level.Info(util_log.Logger).Log("msg", "starting manifest processing", "manifest", manifestPath)

	// Initialize tracking for this manifest
	ctx, cancel := context.WithCancel(ctx)
	b.currentManifestMtx.Lock()
	b.currentManifest = manifestJobs{
		jobsInProgress: make(map[string]jobDetails),
		manifestPath:   manifestPath,
		cancel:         cancel,
	}
	b.currentManifestMtx.Unlock()

	// Process segments sequentially
	for segmentNum := 0; ctx.Err() == nil && segmentNum < manifest.SegmentsCount; segmentNum++ {
		level.Info(util_log.Logger).Log("msg", "starting segment processing",
			"manifest", manifestPath,
			"segment", segmentNum)

		segmentPath := path.Join(manifestPath, fmt.Sprintf("%d.json", segmentNum))

		manifestExists, err := b.deleteStoreClient.ObjectExists(ctx, segmentPath)
		if err != nil {
			return err
		}
		if !manifestExists {
			level.Info(util_log.Logger).Log("msg", "manifest does not exist(likely processed already), skipping", "manifest", manifestPath)
			continue
		}

		segment, err := b.getSegment(ctx, segmentPath)
		if err != nil {
			return err
		}

		// Reset job counters for this segment
		b.currentManifestMtx.Lock()
		b.currentManifest.jobsInProgress = make(map[string]jobDetails)
		b.currentManifestMtx.Unlock()

		b.currSegmentStorageUpdates.reset(segment.TableName, segment.UserID)

		// Process each chunks group (same deletion query)
		for i, group := range segment.ChunksGroups {
			// Check if we should stop processing this manifest
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if err := b.createJobsForChunksGroup(ctx, segment.TableName, segment.UserID, fmt.Sprintf("%d", i), group, jobsChan); err != nil {
				return err
			}
		}

		// Wait for all jobs in this segment to complete
		if err := b.waitForSegmentCompletion(ctx); err != nil {
			return err
		}

		// upload the storage updates for the current table
		if err := b.uploadStorageUpdatesForCurrentSegment(ctx, path.Join(manifestPath, fmt.Sprintf("%d%s", segmentNum, storageUpdatesFilenameSuffix))); err != nil {
			return errors.Wrap(err, "failed to upload storage updates")
		}

		// Delete the processed segment
		if err := b.deleteStoreClient.DeleteObject(ctx, segmentPath); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to delete processed segment",
				"segment", segmentPath,
				"error", err)
		}

		level.Info(util_log.Logger).Log("msg", "finished segment processing",
			"manifest", manifestPath,
			"segment", segmentNum)
	}

	level.Info(util_log.Logger).Log("msg", "finished manifest processing", "manifest", manifestPath)
	return nil
}

// uploadStorageUpdatesForCurrentSegment uploads the storage updates for the currently processed segment to the object storage
func (b *JobBuilder) uploadStorageUpdatesForCurrentSegment(ctx context.Context, path string) error {
	storageUpdatesJSON, err := b.currSegmentStorageUpdates.encode()
	if err != nil {
		return err
	}

	return b.deleteStoreClient.PutObject(ctx, path, bytes.NewReader(storageUpdatesJSON))
}

func (b *JobBuilder) waitForSegmentCompletion(ctx context.Context) error {
	for {
		b.currentManifestMtx.RLock()
		if len(b.currentManifest.jobsInProgress) == 0 {
			b.currentManifestMtx.RUnlock()
			return nil
		}
		b.currentManifestMtx.RUnlock()

		select {
		// ToDo(Sandeep): use timeout config(when introduced) to wait for segment to finish only upto the job timeout.
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			// Check again
		}
	}
}

func (b *JobBuilder) listManifests(ctx context.Context) ([]string, error) {
	// List all directories in the deletion store
	_, commonPrefixes, err := b.deleteStoreClient.List(ctx, "", "/")
	if err != nil {
		return nil, err
	}

	// Filter for manifest directories (they are named with Unix timestamps)
	var manifests []string
	for _, commonPrefix := range commonPrefixes {
		// Check if directory name is a valid timestamp
		if _, err := strconv.ParseInt(path.Base(string(commonPrefix)), 10, 64); err != nil {
			continue
		}

		// Check if manifest.json exists in this directory
		manifestPath := path.Join(string(commonPrefix), manifestFileName)
		exists, err := b.deleteStoreClient.ObjectExists(ctx, manifestPath)
		if err != nil {
			return nil, err
		}
		if !exists {
			// Skip directories without manifest.json
			continue
		}

		manifests = append(manifests, string(commonPrefix))
	}

	return manifests, nil
}

func (b *JobBuilder) readManifest(ctx context.Context, manifestPath string) (*manifest, error) {
	// Read manifest file
	reader, _, err := b.deleteStoreClient.GetObject(ctx, path.Join(manifestPath, manifestFileName))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var m manifest
	if err := json.NewDecoder(reader).Decode(&m); err != nil {
		return nil, err
	}

	return &m, nil
}

func (b *JobBuilder) createJobsForChunksGroup(ctx context.Context, tableName, userID, groupID string, group ChunksGroup, jobsChan chan<- *grpc.Job) error {
	for labels, chunks := range group.Chunks {
		// Split chunks into groups of maxChunksPerJob
		for i := 0; i < len(chunks); i += maxChunksPerJob {
			end := i + maxChunksPerJob
			if end > len(chunks) {
				end = len(chunks)
			}

			payload, err := json.Marshal(&deletionJob{
				TableName:      tableName,
				UserID:         userID,
				ChunkIDs:       chunks[i:end],
				DeleteRequests: group.Requests,
			})
			if err != nil {
				return err
			}

			job := &grpc.Job{
				Id:      fmt.Sprintf("%s_%d", groupID, i/maxChunksPerJob),
				Type:    grpc.JOB_TYPE_DELETION,
				Payload: payload,
			}

			b.currentManifestMtx.Lock()
			b.currentManifest.jobsInProgress[job.Id] = jobDetails{labels: labels}
			b.currentManifestMtx.Unlock()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case jobsChan <- job:
			}
		}
	}

	return nil
}

// OnJobResponse implements jobqueue.Builder interface
func (b *JobBuilder) OnJobResponse(response *grpc.JobResult) error {
	b.currentManifestMtx.Lock()
	defer b.currentManifestMtx.Unlock()

	jobDetails, ok := b.currentManifest.jobsInProgress[response.JobId]
	if !ok {
		return nil
	}

	// Check for job failure
	if response.Error != "" {
		util_log.Logger.Log("msg", "job failed", "job_id", response.JobId, "error", response.Error)
		b.currentManifest.cancel()
		return nil
	}

	var updates storageUpdates
	err := json.Unmarshal(response.Result, &updates)
	if err != nil {
		b.currentManifest.cancel()
		return err
	}

	b.currSegmentStorageUpdates.addUpdates(jobDetails.labels, updates)
	delete(b.currentManifest.jobsInProgress, response.JobId)

	return nil
}

// applyStorageUpdates applies all the storage updates accumulated while processing of the given manifest
func (b *JobBuilder) applyStorageUpdates(ctx context.Context, manifest *manifest, manifestPath string) error {
	storageUpdatesIterator := newStorageUpdatesIterator(ctx, manifestPath, manifest, b.deleteStoreClient)
	return b.applyStorageUpdatesFunc(ctx, storageUpdatesIterator)
}

// cleanupManifest takes care of post-processing cleanup of given manifest which includes:
// 1. Marking all the delete requests in manifest as processed.
// 2. Removing all the object storage files from object storage related to the manifest.
func (b *JobBuilder) cleanupManifest(ctx context.Context, manifest *manifest, manifestPath string) error {
	// mark the delete requests as processed first so that we can move on to processing next requests
	if err := b.markRequestsAsProcessedFunc(append(manifest.Requests, manifest.DuplicateRequests...)); err != nil {
		return err
	}

	// delete the manifest file first so that even if we fail to remove other objects,
	// the current manifest won't get processed again and should get cleaned up in the routine cleanup operation.
	if err := b.deleteStoreClient.DeleteObject(ctx, path.Join(manifestPath, manifestFileName)); err != nil {
		return err
	}

	objects, _, err := b.deleteStoreClient.List(ctx, manifestPath, "/")
	if err != nil {
		return err
	}

	// delete all the remaining objects
	for _, object := range objects {
		if err := b.deleteStoreClient.DeleteObject(ctx, object.Key); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to delete object", "object", object.Key)
		}
	}

	return nil
}

func (b *JobBuilder) getSegment(ctx context.Context, segmentPath string) (*segment, error) {
	reader, _, err := b.deleteStoreClient.GetObject(ctx, segmentPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var segment segment
	if err := json.NewDecoder(reader).Decode(&segment); err != nil {
		return nil, err
	}

	return &segment, nil
}

type storageUpdates struct {
	ChunksToDelete  []string // List of chunks to be deleted from object storage and removed from the index of the current table
	ChunksToDeIndex []string // List of chunks only to be removed from the index of the current table
	ChunksToIndex   []Chunk  // List of chunks to be indexed in the current table
}

// storageUpdatesCollection collects updates to be made to the storage for a single segment
type storageUpdatesCollection struct {
	TableName, UserID string

	mtx            sync.Mutex
	StorageUpdates map[string]*storageUpdates // labels -> storageUpdates mapping
}

func (i *storageUpdatesCollection) reset(tableName, userID string) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	i.TableName = tableName
	i.UserID = userID
	i.StorageUpdates = make(map[string]*storageUpdates)
}

func (i *storageUpdatesCollection) addUpdates(labels string, result storageUpdates) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	updates, ok := i.StorageUpdates[labels]
	if !ok {
		updates = &storageUpdates{}
		i.StorageUpdates[labels] = updates
	}

	updates.ChunksToDelete = append(updates.ChunksToDelete, result.ChunksToDelete...)
	updates.ChunksToDeIndex = append(updates.ChunksToDeIndex, result.ChunksToDeIndex...)
	updates.ChunksToIndex = append(updates.ChunksToIndex, result.ChunksToIndex...)
}

func (i *storageUpdatesCollection) encode() ([]byte, error) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	return json.Marshal(i)
}

// storageUpdatesIterator helps with iterating through all the storage updates files built while processing of each segment in a manifest
type storageUpdatesIterator struct {
	ctx               context.Context
	manifestPath      string
	manifest          *manifest
	deleteStoreClient client.ObjectClient

	currSegmentNum        int
	currUpdatesCollection *storageUpdatesCollection
	err                   error
}

func newStorageUpdatesIterator(ctx context.Context, manifestPath string, manifest *manifest, deleteStoreClient client.ObjectClient) *storageUpdatesIterator {
	return &storageUpdatesIterator{
		ctx:               ctx,
		manifestPath:      manifestPath,
		manifest:          manifest,
		deleteStoreClient: deleteStoreClient,
		currSegmentNum:    -1,
	}
}

// Next checks if we have more storage update files left to go through.
// It returns false if we have no more files left or if we fail in any operation.
// Any operation failure would set err in the iterator.
func (i *storageUpdatesIterator) Next() bool {
	i.currSegmentNum++
	if i.currSegmentNum >= i.manifest.SegmentsCount {
		return false
	}

	storageUpdatesFilePath := path.Join(i.manifestPath, fmt.Sprintf("%d%s", i.currSegmentNum, storageUpdatesFilenameSuffix))
	var err error
	i.currUpdatesCollection, err = i.getStorageUpdates(storageUpdatesFilePath)
	if err != nil {
		i.err = err
		i.currSegmentNum = -1
		return false
	}

	return true
}

func (i *storageUpdatesIterator) UserID() string {
	if i.currUpdatesCollection == nil {
		return ""
	}
	return i.currUpdatesCollection.UserID
}

func (i *storageUpdatesIterator) TableName() string {
	if i.currUpdatesCollection == nil {
		return ""
	}
	return i.currUpdatesCollection.TableName
}

func (i *storageUpdatesIterator) getStorageUpdates(filepath string) (*storageUpdatesCollection, error) {
	reader, _, err := i.deleteStoreClient.GetObject(i.ctx, filepath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var s storageUpdatesCollection
	if err := json.NewDecoder(reader).Decode(&s); err != nil {
		return nil, err
	}

	return &s, nil
}

// Err returns the error we got while doing any of the operations.
func (i *storageUpdatesIterator) Err() error {
	return i.err
}

// ForEachSeries calls the given callback function for each series in the currently loaded updates collection.
// It passes the labels for the series and updates to apply to the storage.
func (i *storageUpdatesIterator) ForEachSeries(callback func(labels string, chunksToDelete []string, chunksToDeIndex []string, chunksToIndex []Chunk) error) error {
	for labels, updates := range i.currUpdatesCollection.StorageUpdates {
		if err := callback(labels, updates.ChunksToDelete, updates.ChunksToDeIndex, updates.ChunksToIndex); err != nil {
			return err
		}
	}

	return nil
}
