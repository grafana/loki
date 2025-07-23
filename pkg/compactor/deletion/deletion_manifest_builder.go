package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var ErrNoChunksSelectedForDeletion = fmt.Errorf("no chunks selected for deletion")

const (
	maxChunksPerSegment = 100000
	manifestFileName    = "manifest.json"
)

// ChunksGroup holds a group of chunks selected by the same set of requests
type ChunksGroup struct {
	Requests []DeleteRequest     `json:"requests"`
	Chunks   map[string][]string `json:"chunks"` // mapping of series labels to a list of ChunkIDs
}

// segment holds limited chunks(upto maxChunksPerSegment) that needs to be processed.
// It also helps segregate chunks belonging to different users/tables.
type segment struct {
	UserID       string        `json:"user_id"`
	TableName    string        `json:"table_name"`
	ChunksGroups []ChunksGroup `json:"chunk_groups"`
	ChunksCount  int           `json:"chunks_count"`
}

// manifest represents the completion state and summary of discovering chunks which processing for the loaded deleteRequestBatch.
// It serves two purposes:
// 1. Acts as a completion marker indicating all chunks for the given delete requests have been found
// 2. Stores a summary of data stored in segments:
//   - Original and duplicate deletion requests
//   - Total number of segments and chunks to be processed
//
// Once all the segments are processed, Requests and DuplicateRequests in the manifest could be marked as processed.
type manifest struct {
	Requests          []DeleteRequest `json:"requests"`
	DuplicateRequests []DeleteRequest `json:"duplicate_requests"`
	SegmentsCount     int             `json:"segments_count"`
	ChunksCount       int             `json:"chunks_count"`
}

// deletionManifestBuilder helps with building the manifest for listing out which chunks to process for a batch of delete requests.
// It is not meant to be used concurrently.
type deletionManifestBuilder struct {
	deletionManifestStoreClient client.ObjectClient
	deleteRequestBatch          *deleteRequestBatch

	currentSegment            map[uint64]ChunksGroup
	currentSegmentChunksCount int
	currentUserID             string
	currentTableName          string

	allUserRequests    []*DeleteRequest
	deletionInterval   model.Interval
	creationTime       time.Time
	segmentsCount      int
	overallChunksCount int
	logger             log.Logger
}

func newDeletionManifestBuilder(deletionManifestStoreClient client.ObjectClient, deleteRequestBatch *deleteRequestBatch) (*deletionManifestBuilder, error) {
	requestCount := 0
	for _, userRequests := range deleteRequestBatch.deleteRequestsToProcess {
		requestCount += len(userRequests.requests)
	}

	// We use a uint64 as a bit field to track which delete requests apply to each chunk.
	// Since uint64 has 64 bits, we can only handle up to 64 delete requests at a time.
	if requestCount > 64 {
		return nil, fmt.Errorf("only upto 64 delete requests allowed, current count: %d", requestCount)
	}

	now := time.Now()

	builder := &deletionManifestBuilder{
		deletionManifestStoreClient: deletionManifestStoreClient,
		deleteRequestBatch:          deleteRequestBatch,
		currentSegment:              make(map[uint64]ChunksGroup),
		creationTime:                now,
		logger:                      log.With(util_log.Logger, "manifest", now.UnixNano()),
	}

	return builder, nil
}

func (d *deletionManifestBuilder) canSkipSeries(userID []byte, lbls labels.Labels) (bool, error) {
	userIDStr := unsafeGetString(userID)

	userRequests := d.deleteRequestBatch.getAllRequestsForUser(userIDStr)
	if len(userRequests) == 0 {
		return true, fmt.Errorf("no requests loaded for user: %s", userIDStr)
	}

	for _, deleteRequest := range d.deleteRequestBatch.getAllRequestsForUser(userIDStr) {
		// if the delete request touches the series, do not skip it
		if labels.Selector(deleteRequest.matchers).Matches(lbls) {
			return false, nil
		}
	}

	return true, nil
}

// AddSeries adds a series and its chunks to the current segment.
// It flushes the current segment if the user ID or table name changes.
// It also ensures that the current segment does not exceed the maximum number of chunks.
func (d *deletionManifestBuilder) AddSeries(ctx context.Context, tableName string, series retention.Series) error {
	canSkip, err := d.canSkipSeries(series.UserID(), series.Labels())
	if err != nil {
		return err
	}
	if canSkip {
		return nil
	}
	userIDStr := unsafeGetString(series.UserID())
	currentLabels := series.Labels().String()

	if userIDStr != d.currentUserID || tableName != d.currentTableName {
		if err := d.flushCurrentBatch(ctx); err != nil {
			return err
		}
		d.currentSegmentChunksCount = 0
		d.currentSegment = make(map[uint64]ChunksGroup)

		d.currentUserID = string(series.UserID())
		d.currentTableName = tableName
		d.allUserRequests = d.deleteRequestBatch.getAllRequestsForUser(userIDStr)
		d.deletionInterval = d.deleteRequestBatch.getDeletionIntervalForUser(userIDStr)
	}

	var chunksGroupIdentifier uint64
	for _, chk := range series.Chunks() {
		if !intervalsOverlap(d.deletionInterval, model.Interval{
			Start: chk.From,
			End:   chk.Through,
		}) {
			continue
		}
		if d.currentSegmentChunksCount >= maxChunksPerSegment {
			if err := d.flushCurrentBatch(ctx); err != nil {
				return err
			}
			d.currentSegmentChunksCount = 0
			for chunksGroupIdentifier := range d.currentSegment {
				group := d.currentSegment[chunksGroupIdentifier]
				group.Chunks = map[string][]string{}
				d.currentSegment[chunksGroupIdentifier] = group
			}
		}

		// We use a uint64 as a bit field to track which delete requests apply to each chunk.
		chunksGroupIdentifier = 0
		for i, deleteRequest := range d.allUserRequests {
			if !deleteRequest.IsDeleted(series.UserID(), series.Labels(), chk) {
				continue
			}

			chunksGroupIdentifier |= 1 << i
		}

		if chunksGroupIdentifier == 0 {
			continue
		}
		d.currentSegmentChunksCount++

		if _, ok := d.currentSegment[chunksGroupIdentifier]; !ok {
			// Iterate through d.allUserRequests and find which bits are turned on in chunksGroupIdentifier
			var deleteRequests []DeleteRequest
			for i := range d.allUserRequests {
				if chunksGroupIdentifier&(1<<i) != 0 { // Check if the i-th bit is turned on
					deleteRequest := d.allUserRequests[i]
					deleteRequests = append(deleteRequests, DeleteRequest{
						RequestID: deleteRequest.RequestID,
						Query:     deleteRequest.Query,
						StartTime: deleteRequest.StartTime,
						EndTime:   deleteRequest.EndTime,
						UserID:    deleteRequest.UserID,
					})
				}
			}

			d.currentSegment[chunksGroupIdentifier] = ChunksGroup{
				Requests: deleteRequests,
				Chunks:   make(map[string][]string),
			}
		}

		group := d.currentSegment[chunksGroupIdentifier]
		group.Chunks[currentLabels] = append(group.Chunks[currentLabels], chk.ChunkID)
		d.currentSegment[chunksGroupIdentifier] = group
	}

	return nil
}

// Finish flushes the current segment and builds the manifest.
func (d *deletionManifestBuilder) Finish(ctx context.Context) error {
	if err := d.flushCurrentBatch(ctx); err != nil {
		return err
	}

	level.Debug(d.logger).Log("msg", "uploading manifest file after finishing building deletion manifest",
		"total_segments", d.segmentsCount,
		"total_chunks", d.overallChunksCount,
		"total_requests", d.deleteRequestBatch.requestCount(),
	)

	var requests []DeleteRequest
	for userID := range d.deleteRequestBatch.deleteRequestsToProcess {
		for i := range d.deleteRequestBatch.deleteRequestsToProcess[userID].requests {
			requests = append(requests, *d.deleteRequestBatch.deleteRequestsToProcess[userID].requests[i])
		}
	}

	manifestJSON, err := json.Marshal(manifest{
		Requests:          requests,
		DuplicateRequests: d.deleteRequestBatch.duplicateRequests,
		SegmentsCount:     d.segmentsCount,
		ChunksCount:       d.overallChunksCount,
	})
	if err != nil {
		return err
	}

	return d.deletionManifestStoreClient.PutObject(ctx, d.buildObjectKey(manifestFileName), strings.NewReader(unsafeGetString(manifestJSON)))
}

func (d *deletionManifestBuilder) flushCurrentBatch(ctx context.Context) error {
	if d.currentSegmentChunksCount == 0 {
		return nil
	}
	level.Debug(d.logger).Log("msg", "flushing segment",
		"segment_num", d.segmentsCount-1,
		"chunks_count", d.currentSegmentChunksCount,
		"user_id", d.currentUserID,
	)

	b := segment{
		UserID:      d.currentUserID,
		TableName:   d.currentTableName,
		ChunksCount: d.currentSegmentChunksCount,
	}
	for _, group := range d.currentSegment {
		if len(group.Chunks) == 0 {
			continue
		}
		b.ChunksGroups = append(b.ChunksGroups, group)
	}
	if len(b.ChunksGroups) == 0 {
		return nil
	}

	slices.SortFunc(b.ChunksGroups, func(a, b ChunksGroup) int {
		if len(a.Requests) < len(b.Requests) {
			return -1
		} else if len(a.Requests) > len(b.Requests) {
			return 1
		}

		return 0
	})
	batchJSON, err := json.Marshal(b)
	if err != nil {
		return err
	}

	d.segmentsCount++
	d.overallChunksCount += d.currentSegmentChunksCount
	d.currentSegmentChunksCount = 0

	return d.deletionManifestStoreClient.PutObject(ctx, d.buildObjectKey(fmt.Sprintf("%d.json", d.segmentsCount-1)), strings.NewReader(unsafeGetString(batchJSON)))
}

func (d *deletionManifestBuilder) buildObjectKey(filename string) string {
	return path.Join(fmt.Sprint(d.creationTime.UnixNano()), filename)
}

func (d *deletionManifestBuilder) path() string {
	return fmt.Sprint(d.creationTime.UnixNano())
}

func storageHasValidManifest(ctx context.Context, deletionManifestStoreClient client.ObjectClient) (bool, error) {
	// List all directories in the deletion store
	_, commonPrefixes, err := deletionManifestStoreClient.List(ctx, "", "/")
	if err != nil {
		return false, err
	}

	for _, commonPrefix := range commonPrefixes {
		// Check if the directory name is a valid timestamp
		if _, err := strconv.ParseInt(path.Base(string(commonPrefix)), 10, 64); err != nil {
			continue
		}

		// Check if manifest.json exists in this directory
		manifestPath := path.Join(string(commonPrefix), manifestFileName)
		exists, err := deletionManifestStoreClient.ObjectExists(ctx, manifestPath)
		if err != nil {
			return false, err
		}

		if !exists {
			// Skip directories without manifest.json
			continue
		}

		return true, nil
	}

	return false, nil
}

func cleanupInvalidManifests(ctx context.Context, deletionManifestStoreClient client.ObjectClient) error {
	// List all directories in the deletion store
	_, commonPrefixes, err := deletionManifestStoreClient.List(ctx, "", "/")
	if err != nil {
		return err
	}

	var firstErr error

	for _, commonPrefix := range commonPrefixes {
		// Check if the directory name is a valid timestamp
		if _, err := strconv.ParseInt(path.Base(string(commonPrefix)), 10, 64); err != nil {
			continue
		}

		// manifest without manifest.json is considered invalid
		manifestPath := path.Join(string(commonPrefix), manifestFileName)
		exists, err := objectExists(ctx, deletionManifestStoreClient, manifestPath)
		if err != nil {
			return err
		}

		if exists {
			// Skip directories with manifest.json
			continue
		}

		level.Info(util_log.Logger).Log("msg", "cleaning up invalid manifest", "manifest", commonPrefix)

		// delete all the contents of the manifest to clean it up
		objects, _, err := deletionManifestStoreClient.List(ctx, string(commonPrefix), "/")
		if err != nil {
			return err
		}

		// delete all the remaining objects
		for _, object := range objects {
			if err := deletionManifestStoreClient.DeleteObject(ctx, object.Key); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to delete object", "object", object.Key)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

	}

	return firstErr
}

// objectExists checks if an object exists in storage with the given key.
// We can't use ObjectClient.ObjectExists method due to a bug in the GCS object client implementation of Thanos.
// (Sandeep): I will fix the bug upstream and remove this once we have the fix merged.
func objectExists(ctx context.Context, objectClient client.ObjectClient, objectPath string) (bool, error) {
	_, err := objectClient.GetAttributes(ctx, objectPath)
	if err == nil {
		return true, nil
	} else if objectClient.IsObjectNotFoundErr(err) {
		return false, nil
	}

	return false, err
}
