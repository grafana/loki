package deletion

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	storage_chunk "github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type GetChunkClientForTableFunc func(table string) (client.Client, error)

type Chunk interface {
	GetFrom() model.Time
	GetThrough() model.Time
	GetFingerprint() uint64
	GetChecksum() uint32
	GetSize() uint32
	GetEntriesCount() uint32
}

type JobRunner struct {
	chunkProcessingConcurrency int
	getChunkClientForTableFunc GetChunkClientForTableFunc
	metrics                    *deletionJobRunnerMetrics
}

func NewJobRunner(chunkProcessingConcurrency int, getStorageClientForTableFunc GetChunkClientForTableFunc, r prometheus.Registerer) *JobRunner {
	return &JobRunner{
		chunkProcessingConcurrency: chunkProcessingConcurrency,
		getChunkClientForTableFunc: getStorageClientForTableFunc,
		metrics:                    newDeletionJobRunnerMetrics(r),
	}
}

func (jr *JobRunner) Run(ctx context.Context, job *grpc.Job) ([]byte, error) {
	var deletionJob deletionproto.DeletionJob
	var updates = deletionproto.StorageUpdates{
		RebuiltChunks: map[string]*deletionproto.Chunk{},
	}

	if err := proto.Unmarshal(job.Payload, &deletionJob); err != nil {
		return nil, err
	}

	chunkClient, err := jr.getChunkClientForTableFunc(deletionJob.TableName)
	if err != nil {
		return nil, err
	}

	var deleteRequests []*deleteRequest
	for i := range deletionJob.DeleteRequests {
		req, err := newDeleteRequest(deletionJob.DeleteRequests[i], jr.metrics.deletedLinesTotal)
		if err != nil {
			return nil, err
		}
		if !req.logSelectorExpr.HasFilter() {
			return nil, errors.New("deletion query does not contain filter")
		}
		deleteRequests = append(deleteRequests, req)
	}

	tableInterval := retention.ExtractIntervalFromTableName(deletionJob.TableName)
	updatesMtx := sync.Mutex{}
	err = concurrency.ForEachJob(ctx, len(deletionJob.ChunkIDs), jr.chunkProcessingConcurrency, func(ctx context.Context, idx int) error {
		chunkID := deletionJob.ChunkIDs[idx]
		chk, err := storage_chunk.ParseExternalKey(deletionJob.UserID, chunkID)
		if err != nil {
			return err
		}

		// get the chunk from storage
		chks, err := chunkClient.GetChunks(ctx, []storage_chunk.Chunk{chk})
		if err != nil {
			// Do not fail processing of the job when we could not find a chunk in storage.
			// The chunk could have been removed by the main compactor due to retention or a delete request without a line filter.
			if chunkClient.IsChunkNotFoundErr(err) {
				level.Warn(util_log.Logger).Log("msg", "skipping processing missing chunk", "chunk_id", chunkID)
				return nil
			}
			return err
		}

		if len(chks) != 1 {
			return fmt.Errorf("expected 1 entry for chunk %s but found %d in storage", chunkID, len(chks))
		}

		// Build a chain of filters to apply on the chunk to remove data requested for deletion.
		var filterFuncs []filter.Func
		for _, req := range deleteRequests {
			filterFunc, err := req.FilterFunction(chks[0].Metric)
			if err != nil {
				return err
			}

			filterFuncs = append(filterFuncs, filterFunc)
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
				updatesMtx.Lock()
				defer updatesMtx.Unlock()
				updates.RebuiltChunks[chunkID] = nil
				return nil
			}
			return err
		}

		// if no lines were deleted then there is nothing to do
		if !linesDeleted {
			return nil
		}

		facade, ok := newChunkData.(*chunkenc.Facade)
		if !ok {
			return errors.New("invalid chunk type")
		}

		newChunkStart, newChunkEnd := util.RoundToMilliseconds(facade.Bounds())

		// the new chunk is out of range for the table in process, so don't upload and index it
		if newChunkStart > tableInterval.End || newChunkEnd < tableInterval.Start {
			// only remove the index entry from the current table since the new chunk is out of its range
			updatesMtx.Lock()
			defer updatesMtx.Unlock()
			updates.ChunksToDeIndex = append(updates.ChunksToDeIndex, chunkID)
			return nil
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
			return err
		}

		err = chunkClient.PutChunks(ctx, []storage_chunk.Chunk{newChunk})
		if err != nil {
			return err
		}

		// add the new chunk details to the list of chunks to index
		updatesMtx.Lock()
		defer updatesMtx.Unlock()
		updates.RebuiltChunks[chunkID] = &deletionproto.Chunk{
			From:        newChunk.From,
			Through:     newChunk.Through,
			Fingerprint: newChunk.Fingerprint,
			Checksum:    newChunk.Checksum,
			KB:          uint32(math.Round(float64(newChunk.Data.UncompressedSize()) / float64(1<<10))),
			Entries:     uint32(newChunk.Data.Entries()),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	jr.metrics.chunksProcessedTotal.Add(float64(len(deletionJob.ChunkIDs)))
	jobResultProto, err := proto.Marshal(&updates)
	if err != nil {
		return nil, err
	}

	return jobResultProto, nil
}
