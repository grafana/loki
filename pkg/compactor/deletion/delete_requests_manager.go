package deletion

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	statusSuccess = "success"
	statusFail    = "fail"
)

type userDeleteRequests struct {
	requests []*DeleteRequest
	// requestsInterval holds the earliest start time and latest end time considering all the delete requests
	requestsInterval model.Interval
}

type DeleteRequestsManager struct {
	deleteRequestsStore       DeleteRequestsStore
	deleteRequestCancelPeriod time.Duration

	deleteRequestsToProcess    map[string]*userDeleteRequests
	deleteRequestsToProcessMtx sync.Mutex
	duplicateRequests          []DeleteRequest
	metrics                    *deleteRequestsManagerMetrics
	wg                         sync.WaitGroup
	done                       chan struct{}
	batchSize                  int
	limits                     Limits
}

func NewDeleteRequestsManager(store DeleteRequestsStore, deleteRequestCancelPeriod time.Duration, batchSize int, limits Limits, registerer prometheus.Registerer) *DeleteRequestsManager {
	dm := &DeleteRequestsManager{
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
		deleteRequestsToProcess:   map[string]*userDeleteRequests{},
		metrics:                   newDeleteRequestsManagerMetrics(registerer),
		done:                      make(chan struct{}),
		batchSize:                 batchSize,
		limits:                    limits,
	}

	go dm.loop()

	if err := dm.mergeShardedRequests(context.Background()); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to merge sharded requests", "err", err)
	}

	return dm
}

func (d *DeleteRequestsManager) loop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	d.wg.Add(1)
	defer d.wg.Done()

	for {
		select {
		case <-ticker.C:
			if err := d.updateMetrics(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to update metrics", "err", err)
			}
		case <-d.done:
			return
		}
	}
}

func (d *DeleteRequestsManager) Stop() {
	close(d.done)
	d.wg.Wait()
}

// mergeShardedRequests merges the sharded requests back to a single request when we are done with processing all the shards
func (d *DeleteRequestsManager) mergeShardedRequests(ctx context.Context) error {
	deleteGroups, err := d.deleteRequestsStore.GetAllDeleteRequests(context.Background())
	if err != nil {
		return err
	}

	slices.SortFunc(deleteGroups, func(a, b DeleteRequest) int {
		return strings.Compare(a.RequestID, b.RequestID)
	})
	deleteRequests := mergeDeletes(deleteGroups)
	for _, req := range deleteRequests {
		// do not consider requests which do not have an id. Request ID won't be set in some tests or there is a bug in our code for loading requests.
		if req.RequestID == "" {
			level.Error(util_log.Logger).Log("msg", "skipped considering request without an id for merging its shards",
				"user_id", req.UserID,
				"start_time", req.StartTime.Unix(),
				"end_time", req.EndTime.Unix(),
				"query", req.Query,
			)
			continue
		}
		// do not do anything if we are not done with processing all the shards or the number of shards is 1
		if req.Status != StatusProcessed {
			continue
		}

		var idxStart, idxEnd int
		for i := range deleteGroups {
			if req.RequestID == deleteGroups[i].RequestID {
				idxStart = i
				break
			}
		}

		for i := len(deleteGroups) - 1; i > 0; i-- {
			if req.RequestID == deleteGroups[i].RequestID {
				idxEnd = i
				break
			}
		}

		// do not do anything if the number of shards is 1
		if idxStart == idxEnd {
			continue
		}
		reqShards := deleteGroups[idxStart : idxEnd+1]

		level.Info(util_log.Logger).Log("msg", "merging sharded request",
			"request_id", req.RequestID,
			"num_shards", len(reqShards),
			"start_time", req.StartTime.Unix(),
			"end_time", req.EndTime.Unix(),
		)
		if err := d.deleteRequestsStore.MergeShardedRequests(ctx, req, reqShards); err != nil {
			return err
		}
	}

	return nil
}

func (d *DeleteRequestsManager) updateMetrics() error {
	deleteRequests, err := d.deleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	pendingDeleteRequestsCount := 0
	oldestPendingRequestCreatedAt := model.Time(0)

	for _, deleteRequest := range deleteRequests {
		// do not consider requests from users whose delete requests should not be processed as per their config
		processRequest, err := d.shouldProcessRequest(deleteRequest)
		if err != nil {
			return err
		}

		if !processRequest {
			continue
		}

		// adding an extra minute here to avoid a race between cancellation of request and picking up the request for processing
		if deleteRequest.Status != StatusReceived || deleteRequest.CreatedAt.Add(d.deleteRequestCancelPeriod).Add(time.Minute).After(model.Now()) {
			continue
		}

		pendingDeleteRequestsCount++
		if oldestPendingRequestCreatedAt == 0 || deleteRequest.CreatedAt.Before(oldestPendingRequestCreatedAt) {
			oldestPendingRequestCreatedAt = deleteRequest.CreatedAt
		}
	}

	// track age of oldest delete request since they became eligible for processing
	oldestPendingRequestAge := time.Duration(0)
	if oldestPendingRequestCreatedAt != 0 {
		oldestPendingRequestAge = model.Now().Sub(oldestPendingRequestCreatedAt.Add(d.deleteRequestCancelPeriod))
	}
	d.metrics.oldestPendingDeleteRequestAgeSeconds.Set(float64(oldestPendingRequestAge / time.Second))
	d.metrics.pendingDeleteRequestsCount.Set(float64(pendingDeleteRequestsCount))

	return nil
}

func (d *DeleteRequestsManager) loadDeleteRequestsToProcess() error {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	// Reset this first so any errors result in a clear map
	d.deleteRequestsToProcess = map[string]*userDeleteRequests{}

	deleteRequests, err := d.filteredSortedDeleteRequests()
	if err != nil {
		return err
	}

	reqCount := 0
	for i := range deleteRequests {
		deleteRequest := deleteRequests[i]
		maxRetentionInterval := getMaxRetentionInterval(deleteRequest.UserID, d.limits)
		// retention interval 0 means retain the data forever
		if maxRetentionInterval != 0 {
			oldestRetainedLogTimestamp := model.Now().Add(-maxRetentionInterval)
			if deleteRequest.StartTime.Before(oldestRetainedLogTimestamp) && deleteRequest.EndTime.Before(oldestRetainedLogTimestamp) {
				level.Info(util_log.Logger).Log(
					"msg", "Marking delete request with interval beyond retention period as processed",
					"delete_request_id", deleteRequest.RequestID,
					"user", deleteRequest.UserID,
				)
				d.markRequestAsProcessed(deleteRequest)
				continue
			}
		}
		if ur, ok := d.deleteRequestsToProcess[deleteRequest.UserID]; ok {
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
					d.duplicateRequests = append(d.duplicateRequests, deleteRequest)
				}
			}
		}
		if reqCount >= d.batchSize {
			logBatchTruncation(reqCount, len(deleteRequests))
			break
		}

		level.Info(util_log.Logger).Log(
			"msg", "Started processing delete request for user",
			"delete_request_id", deleteRequest.RequestID,
			"user", deleteRequest.UserID,
		)

		deleteRequest.Metrics = d.metrics

		ur := d.requestsForUser(deleteRequest)
		ur.requests = append(ur.requests, &deleteRequest)
		if deleteRequest.StartTime < ur.requestsInterval.Start {
			ur.requestsInterval.Start = deleteRequest.StartTime
		}
		if deleteRequest.EndTime > ur.requestsInterval.End {
			ur.requestsInterval.End = deleteRequest.EndTime
		}
		reqCount++
	}

	return nil
}

func (d *DeleteRequestsManager) filteredSortedDeleteRequests() ([]DeleteRequest, error) {
	deleteRequests, err := d.deleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return nil, err
	}

	deleteRequests, err = d.filteredRequests(deleteRequests)
	if err != nil {
		return nil, err
	}

	sort.Slice(deleteRequests, func(i, j int) bool {
		return deleteRequests[i].StartTime < deleteRequests[j].StartTime
	})

	return deleteRequests, nil
}

func (d *DeleteRequestsManager) filteredRequests(reqs []DeleteRequest) ([]DeleteRequest, error) {
	filtered := make([]DeleteRequest, 0, len(reqs))
	for _, deleteRequest := range reqs {
		// adding an extra minute here to avoid a race between cancellation of request and picking up the request for processing
		if deleteRequest.CreatedAt.Add(d.deleteRequestCancelPeriod).Add(time.Minute).After(model.Now()) {
			continue
		}

		processRequest, err := d.shouldProcessRequest(deleteRequest)
		if err != nil {
			return nil, err
		}

		if !processRequest {
			continue
		}

		filtered = append(filtered, deleteRequest)
	}

	return filtered, nil
}

func (d *DeleteRequestsManager) requestsForUser(dr DeleteRequest) *userDeleteRequests {
	ur, ok := d.deleteRequestsToProcess[dr.UserID]
	if !ok {
		ur = &userDeleteRequests{
			requestsInterval: model.Interval{
				Start: dr.StartTime,
				End:   dr.EndTime,
			},
		}
		d.deleteRequestsToProcess[dr.UserID] = ur
	}
	return ur
}

func logBatchTruncation(size, total int) {
	if size < total {
		level.Info(util_log.Logger).Log(
			"msg", fmt.Sprintf("Processing %d of %d delete requests. More requests will be processed in subsequent compactions", size, total),
		)
	}
}

func (d *DeleteRequestsManager) shouldProcessRequest(dr DeleteRequest) (bool, error) {
	mode, err := deleteModeFromLimits(d.limits, dr.UserID)
	if err != nil {
		level.Error(util_log.Logger).Log(
			"msg", "unable to determine deletion mode for user",
			"user", dr.UserID,
		)
		return false, err
	}

	return mode == deletionmode.FilterAndDelete, nil
}

func (d *DeleteRequestsManager) Expired(ref retention.ChunkEntry, _ model.Time) (bool, filter.Func) {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	userIDStr := unsafeGetString(ref.UserID)
	if d.deleteRequestsToProcess[userIDStr] == nil || !intervalsOverlap(d.deleteRequestsToProcess[userIDStr].requestsInterval, model.Interval{
		Start: ref.From,
		End:   ref.Through,
	}) {
		return false, nil
	}

	var filterFuncs []filter.Func

	for _, deleteRequest := range d.deleteRequestsToProcess[userIDStr].requests {
		isDeleted, ff := deleteRequest.IsDeleted(ref)
		if !isDeleted {
			continue
		}

		if ff == nil {
			level.Info(util_log.Logger).Log(
				"msg", "no chunks to retain: the whole chunk is deleted",
				"delete_request_id", deleteRequest.RequestID,
				"sequence_num", deleteRequest.SequenceNum,
				"user", deleteRequest.UserID,
				"chunkID", string(ref.ChunkID),
			)
			d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(ref.UserID)).Inc()
			return true, nil
		}
		filterFuncs = append(filterFuncs, ff)
	}

	if len(filterFuncs) == 0 {
		return false, nil
	}

	d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(ref.UserID)).Inc()
	return true, func(ts time.Time, s string, structuredMetadata ...labels.Label) bool {
		for _, ff := range filterFuncs {
			if ff(ts, s, structuredMetadata...) {
				return true
			}
		}

		return false
	}
}

func (d *DeleteRequestsManager) MarkPhaseStarted() {
	status := statusSuccess
	if err := d.loadDeleteRequestsToProcess(); err != nil {
		status = statusFail
		level.Error(util_log.Logger).Log("msg", "failed to load delete requests to process", "err", err)
	}
	d.metrics.loadPendingRequestsAttemptsTotal.WithLabelValues(status).Inc()
}

func (d *DeleteRequestsManager) MarkPhaseFailed() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	d.metrics.deletionFailures.WithLabelValues("error").Inc()
	d.deleteRequestsToProcess = map[string]*userDeleteRequests{}
}

func (d *DeleteRequestsManager) MarkPhaseTimedOut() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	d.metrics.deletionFailures.WithLabelValues("timeout").Inc()
	d.deleteRequestsToProcess = map[string]*userDeleteRequests{}
}

func (d *DeleteRequestsManager) markRequestAsProcessed(deleteRequest DeleteRequest) {
	if err := d.deleteRequestsStore.UpdateStatus(context.Background(), deleteRequest, StatusProcessed); err != nil {
		level.Error(util_log.Logger).Log(
			"msg", "failed to mark delete request for user as processed",
			"delete_request_id", deleteRequest.RequestID,
			"sequence_num", deleteRequest.SequenceNum,
			"user", deleteRequest.UserID,
			"err", err,
			"deleted_lines", deleteRequest.DeletedLines,
		)
	} else {
		level.Info(util_log.Logger).Log(
			"msg", "delete request for user marked as processed",
			"delete_request_id", deleteRequest.RequestID,
			"sequence_num", deleteRequest.SequenceNum,
			"user", deleteRequest.UserID,
			"deleted_lines", deleteRequest.DeletedLines,
		)
		d.metrics.deleteRequestsProcessedTotal.WithLabelValues(deleteRequest.UserID).Inc()
	}
}

func (d *DeleteRequestsManager) MarkPhaseFinished() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	for _, userDeleteRequests := range d.deleteRequestsToProcess {
		if userDeleteRequests == nil {
			continue
		}

		for _, deleteRequest := range userDeleteRequests.requests {
			d.markRequestAsProcessed(*deleteRequest)
		}
	}

	for _, req := range d.duplicateRequests {
		level.Info(util_log.Logger).Log("msg", "marking duplicate delete request as processed",
			"delete_request_id", req.RequestID,
			"sequence_num", req.SequenceNum,
			"user", req.UserID,
		)
		d.markRequestAsProcessed(req)
	}

	if err := d.mergeShardedRequests(context.Background()); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to merge sharded requests", "err", err)
	}
}

func (d *DeleteRequestsManager) IntervalMayHaveExpiredChunks(_ model.Interval, userID string) bool {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	// We can't do the overlap check between the passed interval and delete requests interval from a user because
	// if a request is issued just for today and there are chunks spanning today and yesterday then
	// the overlap check would skip processing yesterday's index which would result in the index pointing to deleted chunks.
	if userID != "" {
		return d.deleteRequestsToProcess[userID] != nil
	}

	return len(d.deleteRequestsToProcess) != 0
}

func (d *DeleteRequestsManager) DropFromIndex(_ retention.ChunkEntry, _ model.Time, _ model.Time) bool {
	return false
}

func getMaxRetentionInterval(userID string, limits Limits) time.Duration {
	maxRetention := model.Duration(limits.RetentionPeriod(userID))
	if maxRetention == 0 {
		return 0
	}

	for _, streamRetention := range limits.StreamRetention(userID) {
		if streamRetention.Period == 0 {
			return 0
		}
		if streamRetention.Period > maxRetention {
			maxRetention = streamRetention.Period
		}
	}

	return time.Duration(maxRetention)
}
