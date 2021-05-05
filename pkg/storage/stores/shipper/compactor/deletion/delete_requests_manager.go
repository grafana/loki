package deletion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"

	util_log "github.com/cortexproject/cortex/pkg/util/log"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

type DeleteRequestsStore interface {
	GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error)
	UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error
}

type DeleteRequestsManager struct {
	deleteRequestsStore       DeleteRequestsStore
	deleteRequestCancelPeriod time.Duration

	deleteRequestsToProcess    []DeleteRequest
	deleteRequestsToProcessMtx sync.Mutex
}

func NewDeleteRequestsManager(store DeleteRequestsStore, deleteRequestCancelPeriod time.Duration) *DeleteRequestsManager {
	return &DeleteRequestsManager{
		deleteRequestsStore:       store,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
	}
}

func (d *DeleteRequestsManager) loadDeleteRequestsToProcess() error {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	d.deleteRequestsToProcess = nil
	deleteRequests, err := d.deleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	for _, deleteRequest := range deleteRequests {
		// adding an extra minute here to avoid a race between cancellation of request and picking up the request for processing
		if deleteRequest.CreatedAt.Add(d.deleteRequestCancelPeriod).Add(time.Minute).After(model.Now()) {
			continue
		}
		d.deleteRequestsToProcess = append(d.deleteRequestsToProcess, deleteRequest)
	}

	return nil
}

func (d *DeleteRequestsManager) Expired(ref retention.ChunkEntry, _ model.Time) (bool, []model.Interval) {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	intervalsToRetain := []model.Interval{
		{
			Start: ref.From,
			End:   ref.Through,
		},
	}

	for _, deleteRequest := range d.deleteRequestsToProcess {
		rebuiltIntervals := make([]model.Interval, 0, len(intervalsToRetain))
		for _, interval := range intervalsToRetain {
			entry := ref
			entry.From = interval.Start
			entry.Through = interval.End
			isDeleted, newIntervalsToRetain := deleteRequest.IsDeleted(entry)
			if !isDeleted {
				rebuiltIntervals = append(rebuiltIntervals, interval)
			} else {
				rebuiltIntervals = append(rebuiltIntervals, newIntervalsToRetain...)
			}
		}

		intervalsToRetain = rebuiltIntervals
		if len(intervalsToRetain) == 0 {
			return true, nil
		}
	}

	if len(intervalsToRetain) == 1 && intervalsToRetain[0].Start == ref.From && intervalsToRetain[0].End == ref.Through {
		return false, nil
	}

	return true, intervalsToRetain
}

func (d *DeleteRequestsManager) MarkPhaseStarted() {
	if err := d.loadDeleteRequestsToProcess(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to load delete requests to process", "err", err)
	}
}

func (d *DeleteRequestsManager) MarkPhaseFailed() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	d.deleteRequestsToProcess = nil
}

func (d *DeleteRequestsManager) MarkPhaseFinished() {
	d.deleteRequestsToProcessMtx.Lock()
	defer d.deleteRequestsToProcessMtx.Unlock()

	for _, deleteRequest := range d.deleteRequestsToProcess {
		if err := d.deleteRequestsStore.UpdateStatus(context.Background(), deleteRequest.UserID, deleteRequest.RequestID, StatusProcessed); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to mark delete request %s for user %s as processed", deleteRequest.RequestID, deleteRequest.UserID), "err", err)
		}
	}
}
