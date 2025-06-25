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
	maxChunksPerJob            = 1000
	indexUpdatesFilenameSuffix = `-index-updates.json`
)

type deletionJob struct {
	TableName      string          `json:"table_name"`
	UserID         string          `json:"user_id"`
	ChunkIDs       []string        `json:"chunk_ids"`
	DeleteRequests []DeleteRequest `json:"delete_requests"`
}

type manifestJobs struct {
	jobsInProgress map[string]struct{}
	cancel         context.CancelFunc
	manifestPath   string
}

type JobBuilder struct {
	deleteStoreClient client.ObjectClient

	// Current manifest being processed
	currentManifest    manifestJobs
	currentManifestMtx sync.RWMutex

	currSegmentIndexUpdates *indexUpdates
}

func NewJobBuilder(deleteStoreClient client.ObjectClient) *JobBuilder {
	return &JobBuilder{
		deleteStoreClient:       deleteStoreClient,
		currSegmentIndexUpdates: &indexUpdates{},
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
		if err := b.processManifest(ctx, manifestPath, jobsChan); err != nil {
			return err
		}
	}

	return nil
}

func (b *JobBuilder) processManifest(ctx context.Context, manifestPath string, jobsChan chan<- *grpc.Job) error {
	level.Info(util_log.Logger).Log("msg", "starting manifest processing", "manifest", manifestPath)

	// Read manifest
	manifest, err := b.readManifest(ctx, manifestPath)
	if err != nil {
		return err
	}

	// Initialize tracking for this manifest
	ctx, cancel := context.WithCancel(ctx)
	b.currentManifestMtx.Lock()
	b.currentManifest = manifestJobs{
		jobsInProgress: make(map[string]struct{}),
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
		b.currentManifest.jobsInProgress = make(map[string]struct{})
		b.currentManifestMtx.Unlock()

		b.currSegmentIndexUpdates.reset(segment.TableName)

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

		// update the index updates for the current table
		if err := b.uploadIndexUpdateForCurrentSegment(ctx, path.Join(manifestPath, fmt.Sprintf("%d%s", segmentNum, indexUpdatesFilenameSuffix))); err != nil {
			return errors.Wrap(err, "failed to upload index updates")
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

// uploadIndexUpdateForCurrentSegment uploads the index updates for the currently processed segment to the object storage
func (b *JobBuilder) uploadIndexUpdateForCurrentSegment(ctx context.Context, path string) error {
	indexUpdatesJSON, err := b.currSegmentIndexUpdates.encode()
	if err != nil {
		return err
	}

	return b.deleteStoreClient.PutObject(ctx, path, bytes.NewReader(indexUpdatesJSON))
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
	// Split chunks into groups of maxChunksPerJob
	for i := 0; i < len(group.Chunks); i += maxChunksPerJob {
		end := i + maxChunksPerJob
		if end > len(group.Chunks) {
			end = len(group.Chunks)
		}

		payload, err := json.Marshal(&deletionJob{
			TableName:      tableName,
			UserID:         userID,
			ChunkIDs:       group.Chunks[i:end],
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
		b.currentManifest.jobsInProgress[job.Id] = struct{}{}
		b.currentManifestMtx.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobsChan <- job:
		}
	}

	return nil
}

// OnJobResponse implements jobqueue.Builder interface
func (b *JobBuilder) OnJobResponse(response *grpc.JobResult) error {
	b.currentManifestMtx.Lock()
	defer b.currentManifestMtx.Unlock()

	if _, ok := b.currentManifest.jobsInProgress[response.JobId]; !ok {
		return nil
	}

	// Check for job failure
	if response.Error != "" {
		util_log.Logger.Log("msg", "job failed", "job_id", response.JobId, "error", response.Error)
		b.currentManifest.cancel()
		return nil
	}

	var jobResult JobResult
	err := json.Unmarshal(response.Result, &jobResult)
	if err != nil {
		b.currentManifest.cancel()
		return err
	}

	b.currSegmentIndexUpdates.addUpdates(jobResult)
	delete(b.currentManifest.jobsInProgress, response.JobId)

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

// indexUpdates collects updates to be made to the index for the segment in-process
type indexUpdates struct {
	TableName string

	mtx             sync.Mutex
	ChunksToDelete  []string // List of chunks to be deleted from object storage and removed from the index of the current table
	ChunksToDeIndex []string // List of chunks only to be removed from the index of the current table
	ChunksToIndex   []Chunk  // List of chunks to be indexed in the current table
}

func (i *indexUpdates) reset(tableName string) {
	i.TableName = tableName

	i.ChunksToDelete = i.ChunksToDelete[:0]
	i.ChunksToDeIndex = i.ChunksToDeIndex[:0]
	i.ChunksToIndex = i.ChunksToIndex[:0]
}

func (i *indexUpdates) addUpdates(result JobResult) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	i.ChunksToDelete = append(i.ChunksToDelete, result.ChunksToDelete...)
	i.ChunksToDeIndex = append(i.ChunksToDeIndex, result.ChunksToDeIndex...)
	i.ChunksToIndex = append(i.ChunksToIndex, result.ChunksToIndex...)
}

func (i *indexUpdates) encode() ([]byte, error) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	return json.Marshal(i)
}
