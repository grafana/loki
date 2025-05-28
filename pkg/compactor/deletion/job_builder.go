package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/compactor/jobqueue"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const maxChunksPerJob = 1000

type deletionJob struct {
	ChunkIDs        []string `json:"chunk_ids"`
	DeletionQueries []string `json:"deletion_queries"`
}

type manifestJobs struct {
	jobsInProgress map[string]struct{}
	cancel         context.CancelFunc
	manifestPath   string
}

type manifestError struct {
	JobID string `json:"job_id"`
	Error string `json:"error"`
}

type JobBuilder struct {
	deleteStoreClient client.ObjectClient

	// Current manifest being processed
	currentManifest    manifestJobs
	currentManifestMtx sync.RWMutex
}

func NewJobBuilder(deleteStoreClient client.ObjectClient) *JobBuilder {
	return &JobBuilder{
		deleteStoreClient: deleteStoreClient,
	}
}

// BuildJobs implements jobqueue.Builder interface
func (b *JobBuilder) BuildJobs(ctx context.Context, jobsChan chan<- *jobqueue.Job) error {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		if err := b.buildJobs(ctx, jobsChan); err != nil {
			return err
		}

		// Wait for next tick or context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Continue to next iteration
		}
	}
}

func (b *JobBuilder) buildJobs(ctx context.Context, jobsChan chan<- *jobqueue.Job) error {
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

func (b *JobBuilder) processManifest(ctx context.Context, manifestPath string, jobsChan chan<- *jobqueue.Job) error {
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
	for segmentNum := 1; ctx.Err() == nil && segmentNum <= manifest.SegmentsCount; segmentNum++ {
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

		// Process each chunks group (same deletion query)
		for i, group := range segment.ChunksGroups {
			// Check if we should stop processing this manifest
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if err := b.createJobsForChunksGroup(ctx, fmt.Sprintf("%d", i), group, jobsChan); err != nil {
				return err
			}
		}

		// Wait for all jobs in this segment to complete
		if err := b.waitForSegmentCompletion(ctx); err != nil {
			return err
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

func (b *JobBuilder) waitForSegmentCompletion(ctx context.Context) error {
	for {
		b.currentManifestMtx.RLock()
		if len(b.currentManifest.jobsInProgress) == 0 {
			b.currentManifestMtx.RUnlock()
			return nil
		}
		b.currentManifestMtx.RUnlock()

		select {
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

func (b *JobBuilder) createJobsForChunksGroup(ctx context.Context, groupID string, group ChunksGroup, jobsChan chan<- *jobqueue.Job) error {
	deletionQueries := make([]string, 0, len(group.Requests))
	for _, req := range group.Requests {
		deletionQueries = append(deletionQueries, req.Query)
	}

	// Split chunks into groups of maxChunksPerJob
	for i := 0; i < len(group.Chunks); i += maxChunksPerJob {
		end := i + maxChunksPerJob
		if end > len(group.Chunks) {
			end = len(group.Chunks)
		}

		payload, err := json.Marshal(&deletionJob{
			ChunkIDs:        group.Chunks[i:end],
			DeletionQueries: deletionQueries,
		})
		if err != nil {
			return err
		}

		job := &jobqueue.Job{
			Id:      fmt.Sprintf("%s_%d", groupID, i/maxChunksPerJob),
			Type:    jobqueue.JOB_TYPE_DELETION,
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
func (b *JobBuilder) OnJobResponse(response *jobqueue.ReportJobResultRequest) {
	b.currentManifestMtx.Lock()
	defer b.currentManifestMtx.Unlock()

	if _, ok := b.currentManifest.jobsInProgress[response.JobId]; !ok {
		return
	}

	// Check for job failure
	if response.Error != "" {
		util_log.Logger.Log("msg", "job failed", "job_id", response.JobId, "error", response.Error)
		b.currentManifest.cancel()
		return
	}

	delete(b.currentManifest.jobsInProgress, response.JobId)
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
