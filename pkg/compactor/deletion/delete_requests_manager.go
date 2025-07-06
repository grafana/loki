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
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type DeleteRequestsKind string

const (
	statusSuccess = "success"
	statusFail    = "fail"

	seriesProgressFilename = "series_progress.json"

	DeleteRequestsWithLineFilters    DeleteRequestsKind = "DeleteRequestsWithLineFilters"
	DeleteRequestsWithoutLineFilters DeleteRequestsKind = "DeleteRequestsWithoutLineFilters"
	DeleteRequestsAll                DeleteRequestsKind = "DeleteRequestsAll"
)

type userDeleteRequests struct {
	requests []*DeleteRequest
	// requestsInterval holds the earliest start time and latest end time considering all the delete requests
	requestsInterval model.Interval
}

type Table interface {
	GetUserIndex(userID string) (retention.SeriesIterator, error)
}

type TablesManager interface {
	ApplyStorageUpdates(ctx context.Context, iterator StorageUpdatesIterator) error
	IterateTables(ctx context.Context, callback func(string, Table) error) (err error)
}

type TableIteratorFunc func(ctx context.Context, callback func(string, Table) error) (err error)

type DeleteRequestsManager struct {
	workingDir                string
	deleteRequestsStore       DeleteRequestsStore
	deleteRequestCancelPeriod time.Duration

	HSModeEnabled       bool
	deletionStoreClient client.ObjectClient
	jobBuilder          *JobBuilder
	tablesManager       TablesManager

	metrics            *deleteRequestsManagerMetrics
	wg                 sync.WaitGroup
	batchSize          int
	limits             Limits
	currentBatch       *deleteRequestBatch
	processedSeries    map[string]struct{}
	processedSeriesMtx sync.RWMutex
}

func NewDeleteRequestsManager(
	workingDir string,
	store DeleteRequestsStore,
	deleteRequestCancelPeriod time.Duration,
	batchSize int,
	limits Limits,
	HSModeEnabled bool,
	deletionStoreClient client.ObjectClient,
	registerer prometheus.Registerer,
) (*DeleteRequestsManager, error) {
	metrics := newDeleteRequestsManagerMetrics(registerer)
	dm := &DeleteRequestsManager{
		workingDir:                workingDir,
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,

		HSModeEnabled:       HSModeEnabled,
		deletionStoreClient: deletionStoreClient,

		metrics:         metrics,
		batchSize:       batchSize,
		limits:          limits,
		processedSeries: map[string]struct{}{},
		currentBatch:    newDeleteRequestBatch(metrics),
	}

	return dm, nil
}

func (d *DeleteRequestsManager) Init(tablesManager TablesManager) error {
	d.tablesManager = tablesManager

	if d.HSModeEnabled {
		d.jobBuilder = NewJobBuilder(d.deletionStoreClient, tablesManager.ApplyStorageUpdates, func(requests []DeleteRequest) {
			for _, req := range requests {
				d.markRequestAsProcessed(req)
			}
		})
	}

	var err error
	d.processedSeries, err = loadSeriesProgress(d.workingDir)
	if err != nil {
		return err
	}

	if err := d.deleteRequestsStore.MergeShardedRequests(context.Background()); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to merge sharded requests", "err", err)
	}

	return nil
}

// Start starts the DeleteRequestsManager's background operations. It is a blocking call.
// To stop the background operations, cancel the passed context.
func (d *DeleteRequestsManager) Start(ctx context.Context) {
	d.wg.Add(1)
	go d.loop(ctx)

	if d.HSModeEnabled {
		d.wg.Add(1)
		go d.buildDeletionManifestLoop(ctx)
	}

	d.wg.Wait()
	if err := d.storeSeriesProgress(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to store series progress", "err", err)
	}
}

func (d *DeleteRequestsManager) loop(ctx context.Context) {
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
		case <-ctx.Done():
			return
		}
	}
}

func (d *DeleteRequestsManager) buildDeletionManifestLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	defer d.wg.Done()

	for {
		select {
		case <-ticker.C:
			if err := d.buildDeletionManifest(ctx); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to build deletion manifest", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (d *DeleteRequestsManager) buildDeletionManifest(ctx context.Context) error {
	deleteRequestsBatch, err := d.loadDeleteRequestsToProcess(DeleteRequestsWithLineFilters)
	if err != nil {
		return err
	}

	if deleteRequestsBatch.requestCount() == 0 {
		return nil
	}

	deletionManifestBuilder, err := newDeletionManifestBuilder(d.deletionStoreClient, deleteRequestsBatch)
	if err != nil {
		return err
	}

	userIDs := deleteRequestsBatch.userIDs()
	if err := d.tablesManager.IterateTables(ctx, func(tableName string, table Table) error {
		for _, userID := range userIDs {
			iterator, err := table.GetUserIndex(userID)
			if err != nil {
				return err
			}

			if err := iterator.ForEachSeries(ctx, func(series retention.Series) (err error) {
				return deletionManifestBuilder.AddSeries(ctx, tableName, series)
			}); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return deletionManifestBuilder.Finish(ctx)
}

func (d *DeleteRequestsManager) storeSeriesProgress() error {
	d.processedSeriesMtx.RLock()
	defer d.processedSeriesMtx.RUnlock()

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

func (d *DeleteRequestsManager) loadDeleteRequestsToProcess(kind DeleteRequestsKind) (*deleteRequestBatch, error) {
	batch := newDeleteRequestBatch(d.metrics)

	deleteRequests, err := d.filteredSortedDeleteRequests()
	if err != nil {
		return nil, err
	}

	reqCount := 0
	for i := range deleteRequests {
		deleteRequest := deleteRequests[i]

		if deleteRequest.logSelectorExpr == nil {
			err := deleteRequest.SetQuery(deleteRequest.Query)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to init log selector expr for request_id=%s, user_id=%s", deleteRequest.RequestID, deleteRequest.UserID)
			}
		}
		if kind == DeleteRequestsWithLineFilters && !deleteRequest.logSelectorExpr.HasFilter() {
			continue
		} else if kind == DeleteRequestsWithoutLineFilters && deleteRequest.logSelectorExpr.HasFilter() {
			continue
		}
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
		if err := batch.checkDuplicate(deleteRequest); err != nil {
			return nil, err
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

		batch.addDeleteRequest(&deleteRequest)
		reqCount++
	}

	return batch, nil
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
	d.processedSeriesMtx.RLock()
	defer d.processedSeriesMtx.RUnlock()

	userIDStr := unsafeGetString(userID)

	for _, deleteRequest := range d.currentBatch.getAllRequestsForUser(userIDStr) {
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
	return d.currentBatch.expired(userID, chk, lbls, func(request *DeleteRequest) bool {
		d.processedSeriesMtx.RLock()
		defer d.processedSeriesMtx.RUnlock()

		_, ok := d.processedSeries[buildProcessedSeriesKey(request.RequestID, request.StartTime, request.EndTime, seriesID, tableName)]
		return ok
	})
}

func (d *DeleteRequestsManager) MarkPhaseStarted() {
	status := statusSuccess
	loadRequestsKind := DeleteRequestsAll
	if d.HSModeEnabled {
		loadRequestsKind = DeleteRequestsWithoutLineFilters
	}
	if batch, err := d.loadDeleteRequestsToProcess(loadRequestsKind); err != nil {
		status = statusFail
		d.currentBatch = newDeleteRequestBatch(d.metrics)
		level.Error(util_log.Logger).Log("msg", "failed to load delete requests to process", "err", err)
	} else {
		d.currentBatch = batch
	}
	d.metrics.loadPendingRequestsAttemptsTotal.WithLabelValues(status).Inc()
}

func (d *DeleteRequestsManager) MarkPhaseFailed() {
	d.currentBatch.reset()
	d.metrics.deletionFailures.WithLabelValues("error").Inc()
}

func (d *DeleteRequestsManager) MarkPhaseTimedOut() {
	d.currentBatch.reset()
	d.metrics.deletionFailures.WithLabelValues("timeout").Inc()
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
	if d.currentBatch.requestCount() == 0 {
		return
	}

	for _, userDeleteRequests := range d.currentBatch.deleteRequestsToProcess {
		if userDeleteRequests == nil {
			continue
		}

		for _, deleteRequest := range userDeleteRequests.requests {
			d.markRequestAsProcessed(*deleteRequest)
		}
	}

	for _, req := range d.currentBatch.duplicateRequests {
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

	d.processedSeriesMtx.Lock()
	defer d.processedSeriesMtx.Unlock()

	d.processedSeries = map[string]struct{}{}
	d.currentBatch.reset()
	if err := os.Remove(filepath.Join(d.workingDir, seriesProgressFilename)); err != nil && !os.IsNotExist(err) {
		level.Error(util_log.Logger).Log("msg", "failed to remove series progress file", "err", err)
	}
}

func (d *DeleteRequestsManager) IntervalMayHaveExpiredChunks(_ model.Interval, userID string) bool {
	if d.currentBatch.requestCount() == 0 {
		return false
	}

	return d.currentBatch.intervalMayHaveExpiredChunks(userID)
}

func (d *DeleteRequestsManager) DropFromIndex(_ []byte, _ retention.Chunk, _ labels.Labels, _ model.Time, _ model.Time) bool {
	return false
}

func (d *DeleteRequestsManager) MarkSeriesAsProcessed(userID, seriesID []byte, lbls labels.Labels, tableName string) error {
	userIDStr := unsafeGetString(userID)
	if d.currentBatch.requestCount() == 0 {
		return nil
	}

	d.processedSeriesMtx.Lock()
	defer d.processedSeriesMtx.Unlock()

	for _, req := range d.currentBatch.getAllRequestsForUser(userIDStr) {
		// if the delete request does not touch the series, do not waste space in storing the marker
		if !labels.Selector(req.matchers).Matches(lbls) {
			continue
		}
		processedSeriesKey := buildProcessedSeriesKey(req.RequestID, req.StartTime, req.EndTime, seriesID, tableName)
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
