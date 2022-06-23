package deletion

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	statusSuccess = "success"
	statusFail    = "fail"
)

type DeleteRequestsManager struct {
	deleteRequestsStore       DeleteRequestsStore
	deleteRequestCancelPeriod time.Duration

	deleteRequestsToProcess []DeleteRequest
	chunkIntervalsToRetain  []retention.IntervalFilter
	// WARN: If by any chance we change deleteRequestsToProcessMtx to sync.RWMutex to be able to check multiple chunks at a time,
	// please take care of chunkIntervalsToRetain which should be unique per chunk.
	deleteRequestsToProcessMtx sync.Mutex
	metrics                    *deleteRequestsManagerMetrics
	wg                         sync.WaitGroup
	done                       chan struct{}
	deletionMode               Mode
}

func NewDeleteRequestsManager(store DeleteRequestsStore, deleteRequestCancelPeriod time.Duration, registerer prometheus.Registerer, mode Mode) *DeleteRequestsManager {
	dm := &DeleteRequestsManager{
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
		metrics:                   newDeleteRequestsManagerMetrics(registerer),
		done:                      make(chan struct{}),
		deletionMode:              mode,
	}

	go dm.loop()

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

func (d *DeleteRequestsManager) updateMetrics() error {
	deleteRequests, err := d.deleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	pendingDeleteRequestsCount := 0
	oldestPendingRequestCreatedAt := model.Time(0)

	for _, deleteRequest := range deleteRequests {
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

	d.deleteRequestsToProcess = d.deleteRequestsToProcess[:0]
	deleteRequests, err := d.deleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	for _, deleteRequest := range deleteRequests {
		// adding an extra minute here to avoid a race between cancellation of request and picking up the request for processing
		if deleteRequest.CreatedAt.Add(d.deleteRequestCancelPeriod).Add(time.Minute).After(model.Now()) {
			continue
		}
		deleteRequest.deletedLinesTotal = d.metrics.deletedLinesTotal.WithLabelValues(deleteRequest.UserID)
		d.deleteRequestsToProcess = append(d.deleteRequestsToProcess, deleteRequest)
	}

	return nil
}

func (d *DeleteRequestsManager) Expired(ref retention.ChunkEntry, _ model.Time) (bool, []retention.IntervalFilter) {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	if len(d.deleteRequestsToProcess) == 0 {
		return false, nil
	}

	if d.deletionMode == Disabled || d.deletionMode == FilterOnly {
		// Don't process deletes
		return false, nil
	}

	d.chunkIntervalsToRetain = d.chunkIntervalsToRetain[:0]
	d.chunkIntervalsToRetain = append(d.chunkIntervalsToRetain, retention.IntervalFilter{
		Interval: model.Interval{
			Start: ref.From,
			End:   ref.Through,
		},
	})

	for _, deleteRequest := range d.deleteRequestsToProcess {
		level.Info(util_log.Logger).Log(
			"msg", "started processing delete request",
			"delete_request_id", deleteRequest.RequestID,
			"user", deleteRequest.UserID,
		)
		rebuiltIntervals := make([]retention.IntervalFilter, 0, len(d.chunkIntervalsToRetain))
		for _, ivf := range d.chunkIntervalsToRetain {
			entry := ref
			entry.From = ivf.Interval.Start
			entry.Through = ivf.Interval.End
			isDeleted, newIntervalsToRetain := deleteRequest.IsDeleted(entry)
			if !isDeleted {
				rebuiltIntervals = append(rebuiltIntervals, ivf)
			} else {
				rebuiltIntervals = append(rebuiltIntervals, newIntervalsToRetain...)
			}
		}

		d.chunkIntervalsToRetain = rebuiltIntervals
		if len(d.chunkIntervalsToRetain) == 0 {
			level.Info(util_log.Logger).Log(
				"msg", "no chunks to retain: the whole chunk is deleted",
				"delete_request_id", deleteRequest.RequestID,
				"user", deleteRequest.UserID,
				"chunkID", string(ref.ChunkID),
			)
			d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(ref.UserID)).Inc()
			return true, nil
		}
		level.Info(util_log.Logger).Log(
			"msg", "finished processing delete request",
			"delete_request_id", deleteRequest.RequestID,
			"user", deleteRequest.UserID,
		)
	}

	if len(d.chunkIntervalsToRetain) == 1 && d.chunkIntervalsToRetain[0].Interval.Start == ref.From && d.chunkIntervalsToRetain[0].Interval.End == ref.Through {
		return false, nil
	}

	d.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(string(ref.UserID)).Inc()
	return true, d.chunkIntervalsToRetain
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

	d.deleteRequestsToProcess = d.deleteRequestsToProcess[:0]
}

func (d *DeleteRequestsManager) MarkPhaseFinished() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	for _, deleteRequest := range d.deleteRequestsToProcess {
		if err := d.deleteRequestsStore.UpdateStatus(context.Background(), deleteRequest.UserID, deleteRequest.RequestID, StatusProcessed); err != nil {
			level.Error(util_log.Logger).Log(
				"msg", "failed to mark delete request for user as processed",
				"delete_request_id", deleteRequest.RequestID,
				"user", deleteRequest.UserID,
				"err", err,
			)
		} else {
			level.Info(util_log.Logger).Log(
				"msg", "delete request for user marked as processed",
				"delete_request_id", deleteRequest.RequestID,
				"user", deleteRequest.UserID,
			)
		}
		d.metrics.deleteRequestsProcessedTotal.WithLabelValues(deleteRequest.UserID).Inc()
	}
}

func (d *DeleteRequestsManager) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	if userID != "" {
		for _, deleteRequest := range d.deleteRequestsToProcess {
			if deleteRequest.UserID == userID &&
				deleteRequest.StartTime <= interval.End &&
				deleteRequest.EndTime >= interval.Start {
				return true
			}
		}

		return false
	}

	// If your request includes just today and there are chunks spanning today and yesterday then
	// with previous check it won’t process yesterday’s index.
	return len(d.deleteRequestsToProcess) != 0
}

func (d *DeleteRequestsManager) DropFromIndex(_ retention.ChunkEntry, _ model.Time, _ model.Time) bool {
	return false
}
