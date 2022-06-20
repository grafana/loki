package deletion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const deletionNotAvailableMsg = "deletion is not available for this tenant"

// DeleteRequestHandler provides handlers for delete requests
type DeleteRequestHandler struct {
	deleteRequestsStore       DeleteRequestsStore
	metrics                   *deleteRequestHandlerMetrics
	limits                    retention.Limits
	deleteRequestCancelPeriod time.Duration
}

// NewDeleteRequestHandler creates a DeleteRequestHandler
func NewDeleteRequestHandler(deleteStore DeleteRequestsStore, deleteRequestCancelPeriod time.Duration, limits retention.Limits, registerer prometheus.Registerer) *DeleteRequestHandler {
	deleteMgr := DeleteRequestHandler{
		deleteRequestsStore:       deleteStore,
		deleteRequestCancelPeriod: deleteRequestCancelPeriod,
		limits:                    limits,
		metrics:                   newDeleteRequestHandlerMetrics(registerer),
	}

	return &deleteMgr
}

// AddDeleteRequestHandler handles addition of a new delete request
func (dm *DeleteRequestHandler) AddDeleteRequestHandler() http.Handler {
	return dm.deletionMiddleware(http.HandlerFunc(dm.addDeleteRequestHandler))
}

// AddDeleteRequestHandler handles addition of a new delete request
func (dm *DeleteRequestHandler) addDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := r.URL.Query()
	query := params.Get("query")
	if len(query) == 0 {
		http.Error(w, "query not set", http.StatusBadRequest)
		return
	}

	_, err = parseDeletionQuery(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
			http.Error(w, "deletes in the future are not allowed", http.StatusBadRequest)
			return
		}
	}

	if startTime > endTime {
		http.Error(w, "start time can't be greater than end time", http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.AddDeleteRequest(ctx, userID, model.Time(startTime), model.Time(endTime), query); err != nil {
		level.Error(util_log.Logger).Log("msg", "error adding delete request to the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dm.metrics.deleteRequestsReceivedTotal.WithLabelValues(userID).Inc()
	w.WriteHeader(http.StatusNoContent)
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler() http.Handler {
	return dm.deletionMiddleware(http.HandlerFunc(dm.getAllDeleteRequestsHandler))
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) getAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
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
func (dm *DeleteRequestHandler) CancelDeleteRequestHandler() http.Handler {
	return dm.deletionMiddleware(http.HandlerFunc(dm.cancelDeleteRequestHandler))
}

// CancelDeleteRequestHandler handles delete request cancellation
func (dm *DeleteRequestHandler) cancelDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
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

// GetCacheGenerationNumberHandler handles requests for a user's cache generation number
func (dm *DeleteRequestHandler) GetCacheGenerationNumberHandler() http.Handler {
	return dm.deletionMiddleware(http.HandlerFunc(dm.getCacheGenerationNumberHandler))
}

// GetCacheGenerationNumberHandler handles requests for a user's cache generation number
func (dm *DeleteRequestHandler) getCacheGenerationNumberHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cacheGenNumber, err := dm.deleteRequestsStore.GetCacheGenerationNumber(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting cache generation number", "err", err)
		http.Error(w, fmt.Sprintf("error getting cache generation number %v", err), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(cacheGenNumber); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
	}
}

func (dm *DeleteRequestHandler) deletionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		allLimits := dm.limits.AllByUserID()
		userLimits, ok := allLimits[userID]
		if ok {
			if !userLimits.CompactorDeletionEnabled {
				http.Error(w, deletionNotAvailableMsg, http.StatusForbidden)
				return
			}
		} else {
			if !dm.limits.DefaultLimits().CompactorDeletionEnabled {
				http.Error(w, deletionNotAvailableMsg, http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
