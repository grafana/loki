package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
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

	seriesProgressFilename = "series_progress.json"
)

type userDeleteRequests struct {
	requests []*DeleteRequest
	// requestsInterval holds the earliest start time and latest end time considering all the delete requests
	requestsInterval model.Interval
}

type DeleteRequestsManager struct {
	workingDir                string
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
	processedSeries            map[string]struct{}
}

func NewDeleteRequestsManager(workingDir string, store DeleteRequestsStore, deleteRequestCancelPeriod time.Duration, batchSize int, limits Limits, registerer prometheus.Registerer) (*DeleteRequestsManager, error) {
	dm := &DeleteRequestsManager{
		workingDir:                workingDir,
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
		deleteRequestsToProcess:   map[string]*userDeleteRequests{},
		metrics:                   newDeleteRequestsManagerMetrics(registerer),
		done:                      make(chan struct{}),
		batchSize:                 batchSize,
		limits:                    limits,
		processedSeries:           map[string]struct{}{},
	}

	var err error
	dm.processedSeries, err = loadSeriesProgress(workingDir)
	if err != nil {
		return nil, err
	}

	dm.wg.Add(1)
	go dm.loop()

	if err := dm.deleteRequestsStore.MergeShardedRequests(context.Background()); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to merge sharded requests", "err", err)
	}

	return dm, nil
}

func (d *DeleteRequestsManager) loop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	defer d.wg.Done()

	for {
		select {
		case <-ticker.C:
			if err := d.updateMetrics(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to update metrics", "err", err)
			}

			if err := d.storeSeriesProgress(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to store series progress", "err", err)
			}
		case <-d.done:
			return
		}
	}
}

func (d *DeleteRequestsManager) Stop() {
	close(d.done)
	d.wg.Wait()
	if err := d.storeSeriesProgress(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to store series progress", "err", err)
	}
}

func (d *DeleteRequestsManager) storeSeriesProgress() error {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	if len(d.processedSeries) == 0 {
		return nil
	}

	data, err := json.Marshal(d.processedSeries)
	if err != nil {
		return errors.Wrap(err, "failed to json encode series progress")
	}

	err = os.WriteFile(filepath.Join(d.workingDir, seriesProgressFilename), data, 0640)
	if err != nil {
		return errors.Wrap(err, "failed to store series progress to the file")
	}

	return nil
}

func (d *DeleteRequestsManager) updateMetrics() error {
	deleteRequests, err := d.deleteRequestsStore.GetUnprocessedShards(context.Background())
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

		if deleteRequest.logSelectorExpr == nil {
			err := deleteRequest.SetQuery(deleteRequest.Query)
			if err != nil {
				return errors.Wrapf(err, "failed to init log selector expr for request_id=%s, user_id=%s", deleteRequest.RequestID, deleteRequest.UserID)
			}
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
	deleteRequests, err := d.deleteRequestsStore.GetUnprocessedShards(context.Background())
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

func (d *DeleteRequestsManager) CanSkipSeries(userID []byte, lbls labels.Labels, seriesID []byte, _ model.Time, tableName string, _ model.Time) bool {
	userIDStr := unsafeGetString(userID)

	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	if d.deleteRequestsToProcess[userIDStr] == nil {
		return true
	}

	for _, deleteRequest := range d.deleteRequestsToProcess[userIDStr].requests {
		// if the delete request does not touch the series, continue looking for other matching requests
		if !labels.Selector(deleteRequest.matchers).Matches(lbls) {
			continue
		}

		// The delete request touches the series. Do not skip if the series is not processed yet.
		if _, ok := d.processedSeries[buildProcessedSeriesKey(deleteRequest.RequestID, deleteRequest.StartTime, deleteRequest.EndTime, seriesID, tableName)]; !ok {
			return false
		}
	}

	return true
}

func (d *DeleteRequestsManager) Expired(userID []byte, chk retention.Chunk, lbls labels.Labels, seriesID []byte, tableName string, _ model.Time) (bool, filter.Func) {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	userIDStr := unsafeGetString(userID)
	if d.deleteRequestsToProcess[userIDStr] == nil || !intervalsOverlap(d.deleteRequestsToProcess[userIDStr].requestsInterval, model.Interval{
		Start: chk.From,
		End:   chk.Through,
	}) {
		return false, nil
	}

	var filterFuncs []filter.Func

	for _, deleteRequest := range d.deleteRequestsToProcess[userIDStr].requests {
		if _, ok := d.processedSeries[buildProcessedSeriesKey(deleteRequest.RequestID, deleteRequest.StartTime, deleteRequest.EndTime, seriesID, tableName)]; ok {
			continue
		}
		isDeleted, ff := deleteRequest.IsDeleted(userID, lbls, chk)
		if !isDeleted {
			continue
		}

		if ff == nil {
			level.Info(util_log.Logger).Log(
				"msg", "no chunks to retain: the whole chunk is deleted",
				"delete_request_id", deleteRequest.RequestID,
				"sequence_num", deleteRequest.SequenceNum,
				"user", deleteRequest.UserID,
				"chunkID", string(chk.ChunkID),
			)
			d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(userID)).Inc()
			return true, nil
		}
		filterFuncs = append(filterFuncs, ff)
	}

	if len(filterFuncs) == 0 {
		return false, nil
	}

	d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(userID)).Inc()
	return true, func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
		for _, ff := range filterFuncs {
			if ff(ts, s, structuredMetadata) {
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
	if err := d.deleteRequestsStore.MarkShardAsProcessed(context.Background(), deleteRequest); err != nil {
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

	if err := d.deleteRequestsStore.MergeShardedRequests(context.Background()); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to merge sharded requests", "err", err)
	}

	// When we hit a timeout, MarkPhaseTimedOut is called to clear the list of delete requests to avoid marking delete requests as processed.
	// Since this method is still called when we hit a timeout, we do not want to drop the progress so that deletion skips the already processed streams.
	if len(d.deleteRequestsToProcess) > 0 {
		d.processedSeries = map[string]struct{}{}
		if err := os.Remove(filepath.Join(d.workingDir, seriesProgressFilename)); err != nil && !os.IsNotExist(err) {
			level.Error(util_log.Logger).Log("msg", "failed to remove series progress file", "err", err)
		}
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

func (d *DeleteRequestsManager) DropFromIndex(_ []byte, _ retention.Chunk, _ labels.Labels, _ model.Time, _ model.Time) bool {
	return false
}

func (d *DeleteRequestsManager) MarkSeriesAsProcessed(userID, seriesID []byte, lbls labels.Labels, tableName string) error {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	userIDStr := unsafeGetString(userID)
	if d.deleteRequestsToProcess[userIDStr] == nil {
		return nil
	}

	for _, req := range d.deleteRequestsToProcess[userIDStr].requests {
		// if the delete request does not touch the series, do not waste space in storing the marker
		if !labels.Selector(req.matchers).Matches(lbls) {
			continue
		}
		processedSeriesKey := buildProcessedSeriesKey(req.RequestID, req.StartTime, req.EndTime, seriesID, tableName)
		if _, ok := d.processedSeries[processedSeriesKey]; ok {
			return fmt.Errorf("series already marked as processed: [table: %s, user: %s, req_id: %s, start: %d, end: %d, series: %s]", tableName, userID, req.RequestID, req.StartTime, req.EndTime, seriesID)
		}
		d.processedSeries[processedSeriesKey] = struct{}{}
	}

	return nil
}

func buildProcessedSeriesKey(requestID string, startTime, endTime model.Time, seriesID []byte, tableName string) string {
	return fmt.Sprintf("%s/%d/%d/%s/%s", requestID, startTime, endTime, tableName, seriesID)
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

func loadSeriesProgress(workingDir string) (map[string]struct{}, error) {
	data, err := os.ReadFile(filepath.Join(workingDir, seriesProgressFilename))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	processedSeries := map[string]struct{}{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &processedSeries); err != nil {
			return nil, err
		}
	}

	return processedSeries, nil
}
