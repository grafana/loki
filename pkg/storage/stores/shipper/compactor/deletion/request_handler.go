package deletion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// DeleteRequestHandler provides handlers for delete requests
type DeleteRequestHandler struct {
	deleteRequestsStore       DeleteRequestsStore
	metrics                   *deleteRequestHandlerMetrics
	deleteRequestCancelPeriod time.Duration
}

// NewDeleteRequestHandler creates a DeleteRequestHandler
func NewDeleteRequestHandler(deleteStore DeleteRequestsStore, deleteRequestCancelPeriod time.Duration, registerer prometheus.Registerer) *DeleteRequestHandler {
	deleteMgr := DeleteRequestHandler{
		deleteRequestsStore:       deleteStore,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
		metrics:                   newDeleteRequestHandlerMetrics(registerer),
	}

	return &deleteMgr
}

// AddDeleteRequestHandler handles addition of new delete request
func (dm *DeleteRequestHandler) AddDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.ID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := r.URL.Query()
	match := params["match[]"]
	if len(match) == 0 {
		http.Error(w, "selectors not set", http.StatusBadRequest)
		return
	}

	for i := range match {
		_, err := parser.ParseMetricSelector(match[i])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	startParam := params.Get("start")
	startTime := int64(0)
	if startParam != "" {
		startTime, err = util.ParseTime(startParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	endParam := params.Get("end")
	endTime := int64(model.Now())

	if endParam != "" {
		endTime, err = util.ParseTime(endParam)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if endTime > int64(model.Now()) {
			http.Error(w, "deletes in future not allowed", http.StatusBadRequest)
			return
		}
	}

	if startTime > endTime {
		http.Error(w, "start time can't be greater than end time", http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.AddDeleteRequest(ctx, userID, model.Time(startTime), model.Time(endTime), match); err != nil {
		level.Error(util_log.Logger).Log("msg", "error adding delete request to the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dm.metrics.deleteRequestsReceivedTotal.WithLabelValues(userID).Inc()
	w.WriteHeader(http.StatusNoContent)
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.ID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deleteRequests, err := dm.deleteRequestsStore.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(deleteRequests); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
	}
}

// CancelDeleteRequestHandler handles delete request cancellation
func (dm *DeleteRequestHandler) CancelDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.ID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := r.URL.Query()
	requestID := params.Get("request_id")

	deleteRequest, err := dm.deleteRequestsStore.GetDeleteRequest(ctx, userID, requestID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete request from the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if deleteRequest == nil {
		http.Error(w, "could not find delete request with given id", http.StatusBadRequest)
		return
	}

	if deleteRequest.Status != StatusReceived {
		http.Error(w, "deletion of request which is in process or already processed is not allowed", http.StatusBadRequest)
		return
	}

	if deleteRequest.CreatedAt.Add(dm.deleteRequestCancelPeriod).Before(model.Now()) {
		http.Error(w, fmt.Sprintf("deletion of request past the deadline of %s since its creation is not allowed", dm.deleteRequestCancelPeriod.String()), http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.RemoveDeleteRequest(ctx, userID, requestID, deleteRequest.CreatedAt, deleteRequest.StartTime, deleteRequest.EndTime); err != nil {
		level.Error(util_log.Logger).Log("msg", "error cancelling the delete request", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
