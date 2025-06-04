package deletion

import (
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// deleteRequestBatch holds a batch of requests loaded for processing
type deleteRequestBatch struct {
	deleteRequestsToProcess map[string]*userDeleteRequests
	duplicateRequests       []DeleteRequest
	count                   int
	metrics                 *deleteRequestsManagerMetrics
}

func newDeleteRequestBatch(metrics *deleteRequestsManagerMetrics) *deleteRequestBatch {
	return &deleteRequestBatch{
		deleteRequestsToProcess: map[string]*userDeleteRequests{},
		metrics:                 metrics,
	}
}

func (b *deleteRequestBatch) reset() {
	b.deleteRequestsToProcess = map[string]*userDeleteRequests{}
	b.duplicateRequests = []DeleteRequest{}
	b.count = 0
}

func (b *deleteRequestBatch) requestCount() int {
	return b.count
}

// addDeleteRequest add a requests to the batch
func (b *deleteRequestBatch) addDeleteRequest(dr *DeleteRequest) {
	dr.Metrics = b.metrics
	ur, ok := b.deleteRequestsToProcess[dr.UserID]
	if !ok {
		ur = &userDeleteRequests{
			requestsInterval: model.Interval{
				Start: dr.StartTime,
				End:   dr.EndTime,
			},
		}
		b.deleteRequestsToProcess[dr.UserID] = ur
	}

	ur.requests = append(ur.requests, dr)
	if dr.StartTime < ur.requestsInterval.Start {
		ur.requestsInterval.Start = dr.StartTime
	}
	if dr.EndTime > ur.requestsInterval.End {
		ur.requestsInterval.End = dr.EndTime
	}
	b.count++
}

func (b *deleteRequestBatch) checkDuplicate(deleteRequest DeleteRequest) error {
	ur, ok := b.deleteRequestsToProcess[deleteRequest.UserID]
	if !ok {
		return nil
	}
	for _, requestLoadedForProcessing := range ur.requests {
		isDuplicate, err := requestLoadedForProcessing.IsDuplicate(&deleteRequest)
		if err != nil {
			return err
		}
		if isDuplicate {
			level.Info(util_log.Logger).Log(
				"msg", "found duplicate request of one of the requests loaded for processing",
				"loaded_request_id", requestLoadedForProcessing.RequestID,
				"duplicate_request_id", deleteRequest.RequestID,
				"user", deleteRequest.UserID,
			)
			b.duplicateRequests = append(b.duplicateRequests, deleteRequest)
		}
	}

	return nil
}

func (b *deleteRequestBatch) expired(userID []byte, chk retention.Chunk, lbls labels.Labels, skipRequest func(*DeleteRequest) bool) (bool, filter.Func) {
	userIDStr := unsafeGetString(userID)
	if b.deleteRequestsToProcess[userIDStr] == nil || !intervalsOverlap(b.deleteRequestsToProcess[userIDStr].requestsInterval, model.Interval{
		Start: chk.From,
		End:   chk.Through,
	}) {
		return false, nil
	}

	var filterFuncs []filter.Func

	for _, deleteRequest := range b.deleteRequestsToProcess[userIDStr].requests {
		if skipRequest(deleteRequest) {
			continue
		}
		isDeleted, ff := deleteRequest.GetChunkFilter(userID, lbls, chk)
		if !isDeleted {
			continue
		}

		if ff == nil {
			level.Info(util_log.Logger).Log(
				"msg", "no chunks to retain: the whole chunk is deleted",
				"delete_request_id", deleteRequest.RequestID,
				"sequence_num", deleteRequest.SequenceNum,
				"user", deleteRequest.UserID,
				"chunkID", chk.ChunkID,
			)
			b.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(userID)).Inc()
			return true, nil
		}
		filterFuncs = append(filterFuncs, ff)
	}

	if len(filterFuncs) == 0 {
		return false, nil
	}

	b.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(userID)).Inc()
	return true, func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
		for _, ff := range filterFuncs {
			if ff(ts, s, structuredMetadata) {
				return true
			}
		}

		return false
	}
}

func (b *deleteRequestBatch) intervalMayHaveExpiredChunks(userID string) bool {
	// We can't do the overlap check between the passed interval and delete requests interval from a user because
	// if a request is issued just for today and there are chunks spanning today and yesterday then
	// the overlap check would skip processing yesterday's index which would result in the index pointing to deleted chunks.
	if userID != "" {
		return b.deleteRequestsToProcess[userID] != nil
	}

	return len(b.deleteRequestsToProcess) != 0
}

func (b *deleteRequestBatch) getAllRequestsForUser(userID string) []*DeleteRequest {
	userRequests, ok := b.deleteRequestsToProcess[userID]
	if !ok {
		return nil
	}

	return userRequests.requests
}
