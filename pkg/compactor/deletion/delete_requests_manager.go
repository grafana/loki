package deletion

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type DeleteRequestsKind string

const (
	statusSuccess = "success"
	statusFail    = "fail"

	seriesProgressOldFilename = "series_progress.json"
	seriesProgressFilename    = "series_progress.boltdb"

	DeleteRequestsWithLineFilters    DeleteRequestsKind = "DeleteRequestsWithLineFilters"
	DeleteRequestsWithoutLineFilters DeleteRequestsKind = "DeleteRequestsWithoutLineFilters"
	DeleteRequestsAll                DeleteRequestsKind = "DeleteRequestsAll"
)

var (
	seriesProgressVal = []byte("1")
	boltdbBucketName  = []byte("series_progress")
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

	HSModeEnabled               bool
	deletionManifestStoreClient client.ObjectClient
	jobBuilder                  *JobBuilder
	tablesManager               TablesManager

	metrics      *deleteRequestsManagerMetrics
	wg           sync.WaitGroup
	batchSize    int
	limits       Limits
	currentBatch *deleteRequestBatch

	// seriesProgress file reference could be nil when there are any unexpected issues since
	// series progress tracking is done as best effort.
	// It would also be nil when running compactor in horizontal scaling mode.
	// All the usages should do a nil check.
	seriesProgress *bbolt.DB
}

func NewDeleteRequestsManager(
	workingDir string,
	store DeleteRequestsStore,
	deleteRequestCancelPeriod time.Duration,
	batchSize int,
	limits Limits,
	HSModeEnabled bool,
	deletionManifestStoreClient client.ObjectClient,
	registerer prometheus.Registerer,
) (*DeleteRequestsManager, error) {
	metrics := newDeleteRequestsManagerMetrics(registerer)
	dm := &DeleteRequestsManager{
		workingDir:                workingDir,
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,

		HSModeEnabled:               HSModeEnabled,
		deletionManifestStoreClient: deletionManifestStoreClient,

		metrics:      metrics,
		batchSize:    batchSize,
		limits:       limits,
		currentBatch: newDeleteRequestBatch(metrics),
	}

	// remove the old json series progress file
	if err := os.Remove(filepath.Join(workingDir, seriesProgressOldFilename)); err != nil && !os.IsNotExist(err) {
		level.Error(util_log.Logger).Log("msg", "failed to remove old json series progress file", "err", err)
	}

	return dm, nil
}

func (d *DeleteRequestsManager) Init(tablesManager TablesManager, registerer prometheus.Registerer) error {
	d.tablesManager = tablesManager

	if d.HSModeEnabled {
		d.jobBuilder = NewJobBuilder(d.deletionManifestStoreClient, tablesManager.ApplyStorageUpdates, func(requests []DeleteRequest) {
			for _, req := range requests {
				d.markRequestAsProcessed(req)
			}
		}, registerer)

		// Remove the series progress file since it is only useful when not running compactor in horizontally scalable mode.
		if err := os.Remove(filepath.Join(d.workingDir, seriesProgressFilename)); err != nil && !os.IsNotExist(err) {
			level.Error(util_log.Logger).Log("msg", "failed to remove series progress file", "err", err)
		}
	} else {
		// Open boltdb file for storing series progress.
		db, err := util.SafeOpenBoltdbFile(filepath.Join(d.workingDir, seriesProgressFilename))
		if err != nil {
			return err
		}

		// Create the bucket for storing the KVs
		if err := db.Update(func(tx *bbolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(boltdbBucketName)
			return err
		}); err != nil {
			return err
		}
		d.seriesProgress = db
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

	if d.seriesProgress != nil {
		if err := d.seriesProgress.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close series progress db", "err", err)
		}
	}
}

func (d *DeleteRequestsManager) JobBuilder() *JobBuilder {
	return d.jobBuilder
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
		case <-ctx.Done():
			return
		}
	}
}

func (d *DeleteRequestsManager) buildDeletionManifestLoop(ctx context.Context) {
	if err := cleanupInvalidManifests(ctx, d.deletionManifestStoreClient); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to cleanup invalid delete manifests", "err", err)
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	defer d.wg.Done()

	for {
		select {
		case <-ticker.C:
			status := statusSuccess
			if err := d.buildDeletionManifest(ctx); err != nil {
				status = statusFail
				level.Error(util_log.Logger).Log("msg", "failed to build deletion manifest", "err", err)
			}
			d.metrics.manifestBuildAttemptsTotal.WithLabelValues(status).Inc()
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

	// Do not build another manifest if one already exists since we do not know which requests are already added to it for processing.
	// There is anyway no benefit in building multiple manifests since we process one manifest at a time.
	manifestExists, err := storageHasValidManifest(ctx, d.deletionManifestStoreClient)
	if err != nil {
		return err
	}

	if manifestExists {
		level.Info(util_log.Logger).Log("msg", "skipping building deletion manifest because a valid manifest already exists")
		return nil
	}

	level.Info(util_log.Logger).Log("msg", "building deletion manifest")

	deletionManifestBuilder, err := newDeletionManifestBuilder(d.deletionManifestStoreClient, deleteRequestsBatch)
	if err != nil {
		return err
	}

	level.Info(deletionManifestBuilder.logger).Log("msg", "adding series to deletion manifest")
	userIDs := deleteRequestsBatch.userIDs()
	if err := d.tablesManager.IterateTables(ctx, func(tableName string, table Table) error {
		for _, userID := range userIDs {
			iterator, err := table.GetUserIndex(userID)
			if err != nil {
				return err
			}

			if iterator == nil {
				continue
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

	level.Info(util_log.Logger).Log("msg", "done adding series to deletion manifest")
	err = deletionManifestBuilder.Finish(ctx)
	if err != nil {
		return err
	}
	d.metrics.chunksSelectedTotal.Add(float64(deletionManifestBuilder.overallChunksCount))

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
		isDuplicate, err := batch.checkDuplicate(deleteRequest)
		if err != nil {
			return nil, err
		}
		if isDuplicate {
			continue
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
	userIDStr := unsafeGetString(userID)

	for _, deleteRequest := range d.currentBatch.getAllRequestsForUser(userIDStr) {
		// if the delete request does not touch the series, continue looking for other matching requests
		if !labels.Selector(deleteRequest.matchers).Matches(lbls) {
			continue
		}

		seriesProcessed := d.isSeriesProcessed(buildProcessedSeriesKey(deleteRequest.RequestID, deleteRequest.StartTime, deleteRequest.EndTime, seriesID, tableName))
		// The delete request touches the series. Do not skip if the series is not processed yet.
		if !seriesProcessed {
			return false
		}
	}

	return true
}

func (d *DeleteRequestsManager) Expired(userID []byte, chk retention.Chunk, lbls labels.Labels, seriesID []byte, tableName string, _ model.Time) (bool, filter.Func) {
	return d.currentBatch.expired(userID, chk, lbls, func(request *DeleteRequest) bool {
		return d.isSeriesProcessed(buildProcessedSeriesKey(request.RequestID, request.StartTime, request.EndTime, seriesID, tableName))
	})
}

// isSeriesProcessed checks if the series with given key is processed.
// Returns false if the series progress file reference is nil.
func (d *DeleteRequestsManager) isSeriesProcessed(seriesKey []byte) bool {
	if d.seriesProgress == nil {
		return false
	}
	seriesProcessed := false
	if err := d.seriesProgress.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(boltdbBucketName).Get(seriesKey)
		if len(v) > 0 {
			seriesProcessed = true
		}
		return nil
	}); err != nil {
		level.Error(util_log.Logger).Log("msg", "error checking series progress", "err", err)
	}

	return seriesProcessed
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

	if !d.HSModeEnabled {
		d.resetSeriesProgress()
	}
	d.currentBatch.reset()
}

// resetSeriesProgress removes the existing boltdb file for series progress and creates a new one.
// We remove and recreate the file because boltdb does not reclaim the space when data is deleted.
func (d *DeleteRequestsManager) resetSeriesProgress() {
	// Clear the file reference so that we do not keep using the same file when any of the below cleanup operations fail.
	// Otherwise, we will keep using the same file and end up using too much disk.
	// We check the file reference before we read/write to d.seriesProgress.
	seriesProgress := d.seriesProgress
	d.seriesProgress = nil

	if seriesProgress != nil {
		if err := seriesProgress.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close series progress file", "err", err)
			return
		}
	}

	if err := os.Remove(filepath.Join(d.workingDir, seriesProgressFilename)); err != nil && !os.IsNotExist(err) {
		level.Error(util_log.Logger).Log("msg", "failed to remove series progress file", "err", err)
		return
	}

	db, err := util.SafeOpenBoltdbFile(filepath.Join(d.workingDir, seriesProgressFilename))
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to open series progress", "err", err)
		return
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(boltdbBucketName)
		return err
	})
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to create bucket in series progress boltdb file", "err", err)
		return
	}

	d.seriesProgress = db
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

// MarkSeriesAsProcessed marks a series as processed. It ignores the operation if the series progress file reference is nil.
func (d *DeleteRequestsManager) MarkSeriesAsProcessed(userID, seriesID []byte, lbls labels.Labels, tableName string) error {
	if d.seriesProgress == nil {
		return nil
	}
	userIDStr := unsafeGetString(userID)
	if d.currentBatch.requestCount() == 0 {
		return nil
	}

	for _, req := range d.currentBatch.getAllRequestsForUser(userIDStr) {
		// if the delete request does not touch the series, do not waste space in storing the marker
		if !labels.Selector(req.matchers).Matches(lbls) {
			continue
		}
		processedSeriesKey := buildProcessedSeriesKey(req.RequestID, req.StartTime, req.EndTime, seriesID, tableName)
		if err := d.seriesProgress.Update(func(tx *bbolt.Tx) error {
			return tx.Bucket(boltdbBucketName).Put(processedSeriesKey, seriesProgressVal)
		}); err != nil {
			level.Error(util_log.Logger).Log("msg", "error storing series progress", "err", err)
		}
	}

	return nil
}

func buildProcessedSeriesKey(requestID string, startTime, endTime model.Time, seriesID []byte, tableName string) []byte {
	return unsafeGetBytes(fmt.Sprintf("%s/%d/%d/%s/%s", requestID, startTime, endTime, tableName, seriesID))
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

func unsafeGetBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}
