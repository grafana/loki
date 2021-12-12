package deletion

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/log/level"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"

	serverutil "github.com/grafana/loki/pkg/util/server"
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
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		serverutil.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := r.URL.Query()
	match := params["match[]"]
	if len(match) == 0 {
		serverutil.JSONError(w, http.StatusBadRequest, "selectors not set")
		return
	}

	for i := range match {
		_, err := parser.ParseMetricSelector(match[i])
		if err != nil {
			serverutil.JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	startParam := params.Get("start")
	startTime := int64(0)
	if startParam != "" {
		startTime, err = util.ParseTime(startParam)
		if err != nil {
			serverutil.JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	endParam := params.Get("end")
	endTime := int64(model.Now())

	if endParam != "" {
		endTime, err = util.ParseTime(endParam)
		if err != nil {
			serverutil.JSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		if endTime > int64(model.Now()) {
			serverutil.JSONError(w, http.StatusBadRequest, "deletes in future not allowed")
			return
		}
	}

	if startTime > endTime {
		http.Error(w, "start time can't be greater than end time", http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.AddDeleteRequest(ctx, userID, model.Time(startTime), model.Time(endTime), match); err != nil {
		level.Error(util_log.Logger).Log("msg", "error adding delete request to the store", "err", err)
		serverutil.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	dm.metrics.deleteRequestsReceivedTotal.WithLabelValues(userID).Inc()
	w.WriteHeader(http.StatusNoContent)
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		serverutil.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	deleteRequests, err := dm.deleteRequestsStore.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		serverutil.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := json.NewEncoder(w).Encode(deleteRequests); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		serverutil.JSONError(w, http.StatusInternalServerError, "error marshalling response: %v", err)
	}
}

// CancelDeleteRequestHandler handles delete request cancellation
func (dm *DeleteRequestHandler) CancelDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		serverutil.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	params := r.URL.Query()
	requestID := params.Get("request_id")

	deleteRequest, err := dm.deleteRequestsStore.GetDeleteRequest(ctx, userID, requestID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete request from the store", "err", err)
		serverutil.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if deleteRequest == nil {
		serverutil.JSONError(w, http.StatusBadRequest, "could not find delete request with given id")
		return
	}

	if deleteRequest.Status != StatusReceived {
		serverutil.JSONError(w, http.StatusBadRequest, "deletion of request which is in process or already processed is not allowed")
		return
	}

	if deleteRequest.CreatedAt.Add(dm.deleteRequestCancelPeriod).Before(model.Now()) {
		serverutil.JSONError(w, http.StatusBadRequest, "deletion of request past the deadline of %s since its creation is not allowed", dm.deleteRequestCancelPeriod.String())
		return
	}

	if err := dm.deleteRequestsStore.RemoveDeleteRequest(ctx, userID, requestID, deleteRequest.CreatedAt, deleteRequest.StartTime, deleteRequest.EndTime); err != nil {
		level.Error(util_log.Logger).Log("msg", "error cancelling the delete request", "err", err)
		serverutil.JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
