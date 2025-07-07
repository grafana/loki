package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	storage_chunk "github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type GetChunkClientForTableFunc func(ctx context.Context, table string) (client.Client, error)

type JobRunner struct {
	getChunkClientForTableFunc GetChunkClientForTableFunc
}

type Chunk interface {
	GetFrom() model.Time
	GetThrough() model.Time
	GetFingerprint() uint64
	GetChecksum() uint32
	GetSize() uint32
	GetEntriesCount() uint32
}

type chunk struct {
	From, Through model.Time
	Fingerprint   uint64
	Checksum      uint32
	KB, Entries   uint32
}

func (c chunk) GetFrom() model.Time {
	return c.From
}

func (c chunk) GetThrough() model.Time {
	return c.Through
}

func (c chunk) GetFingerprint() uint64 {
	return c.Fingerprint
}

func (c chunk) GetChecksum() uint32 {
	return c.Checksum
}

func (c chunk) GetSize() uint32 {
	return c.KB
}

func (c chunk) GetEntriesCount() uint32 {
	return c.Entries
}

func NewJobRunner(getStorageClientForTableFunc GetChunkClientForTableFunc) *JobRunner {
	return &JobRunner{
		getChunkClientForTableFunc: getStorageClientForTableFunc,
	}
}

func (jr *JobRunner) Run(ctx context.Context, job *grpc.Job) ([]byte, error) {
	var deletionJob deletionJob
	var updates storageUpdates

	if err := json.Unmarshal(job.Payload, &deletionJob); err != nil {
		return nil, err
	}

	chunkClient, err := jr.getChunkClientForTableFunc(ctx, deletionJob.TableName)
	if err != nil {
		return nil, err
	}

	for i := range deletionJob.DeleteRequests {
		req := &deletionJob.DeleteRequests[i]
		if err := req.SetQuery(req.Query); err != nil {
			return nil, err
		}
		if !req.logSelectorExpr.HasFilter() {
			return nil, errors.New("deletion query does not contain filter")
		}
		// ToDo(Sandeep): set it to proper metrics
		req.Metrics = newDeleteRequestsManagerMetrics(nil)
	}

	var prevFingerprint uint64
	var filterFuncs []filter.Func

	tableInterval := retention.ExtractIntervalFromTableName(deletionJob.TableName)
	// ToDo(Sandeep): Make chunk processing concurrent with a reasonable concurrency.
	for _, chunkID := range deletionJob.ChunkIDs {
		chk, err := storage_chunk.ParseExternalKey(deletionJob.UserID, chunkID)
		if err != nil {
			return nil, err
		}

		// get the chunk from storage
		chks, err := chunkClient.GetChunks(ctx, []storage_chunk.Chunk{chk})
		if err != nil {
			return nil, err
		}

		if len(chks) != 1 {
			return nil, fmt.Errorf("expected 1 entry for chunk %s but found %d in storage", chunkID, len(chks))
		}

		// Build a chain of filters to apply on the chunk to remove data requested for deletion.
		// If we are processing chunk of the same stream then reuse the same filterFuncs as earlier
		if prevFingerprint != chks[0].Fingerprint {
			filterFuncs = filterFuncs[:0]
			prevFingerprint = chks[0].Fingerprint

			for _, req := range deletionJob.DeleteRequests {
				filterFunc, err := req.FilterFunction(chks[0].Metric)
				if err != nil {
					return nil, err
				}

				filterFuncs = append(filterFuncs, filterFunc)
			}
		}

		// rebuild the chunk and see if we excluded any of the lines from the existing chunk
		var linesDeleted bool
		newChunkData, err := chks[0].Data.Rebound(chk.From, chk.Through, func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
			for _, filterFunc := range filterFuncs {
				if filterFunc(ts, s, structuredMetadata) {
					linesDeleted = true
					return true
				}
			}

			return false
		})
		if err != nil {
			if errors.Is(err, storage_chunk.ErrSliceNoDataInRange) {
				level.Info(util_log.Logger).Log("msg", "Delete request filterFunc leaves an empty chunk", "chunk ref", chunkID)
				updates.ChunksToDelete = append(updates.ChunksToDelete, chunkID)
				continue
			}
			return nil, err
		}

		// if no lines were deleted then there is nothing to do
		if !linesDeleted {
			continue
		}

		facade, ok := newChunkData.(*chunkenc.Facade)
		if !ok {
			return nil, errors.New("invalid chunk type")
		}

		newChunkStart, newChunkEnd := util.RoundToMilliseconds(facade.Bounds())

		// the new chunk is out of range for the table in process, so don't upload and index it
		if newChunkStart > tableInterval.End || newChunkEnd < tableInterval.Start {
			// only remove the index entry from the current table since the new chunk is out of its range
			updates.ChunksToDeIndex = append(updates.ChunksToDeIndex, chunkID)
			continue
		}

		// upload the new chunk to object storage
		newChunk := storage_chunk.NewChunk(
			deletionJob.UserID, chks[0].FingerprintModel(), chks[0].Metric,
			facade,
			newChunkStart,
			newChunkEnd,
		)

		err = newChunk.Encode()
		if err != nil {
			return nil, err
		}

		err = chunkClient.PutChunks(ctx, []storage_chunk.Chunk{newChunk})
		if err != nil {
			return nil, err
		}

		// add the new chunk details to the list of chunks to index
		updates.ChunksToIndex = append(updates.ChunksToIndex, chunk{
			From:        newChunk.From,
			Through:     newChunk.Through,
			Fingerprint: newChunk.Fingerprint,
			Checksum:    newChunk.Checksum,
			KB:          uint32(math.Round(float64(newChunk.Data.UncompressedSize()) / float64(1<<10))),
			Entries:     uint32(newChunk.Data.Entries()),
		})

		// Add the ID of original chunk to the list of ChunksToDelete
		updates.ChunksToDelete = append(updates.ChunksToDelete, chunkID)
	}

	jobResultJSON, err := json.Marshal(updates)
	if err != nil {
		return nil, err
	}

	return jobResultJSON, nil
}
