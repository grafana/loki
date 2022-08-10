package deletion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/loki/pkg/util"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/tenant"

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

// AddDeleteRequestHandler handles addition of a new delete request
func (dm *DeleteRequestHandler) AddDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := r.URL.Query()
	query, err := query(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	startTime, err := startTime(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	endTime, err := endTime(params, startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.AddDeleteRequest(ctx, userID, model.Time(startTime), model.Time(endTime), query); err != nil {
		level.Error(util_log.Logger).Log("msg", "error adding delete request to the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	level.Info(util_log.Logger).Log(
		"msg", "delete request for user added",
		"user", userID,
		"query", query,
	)

	dm.metrics.deleteRequestsReceivedTotal.WithLabelValues(userID).Inc()
	w.WriteHeader(http.StatusNoContent)
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
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
func (dm *DeleteRequestHandler) CancelDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
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

	if err := dm.deleteRequestsStore.RemoveDeleteRequest(ctx, userID, requestID, deleteRequest.CreatedAt, deleteRequest.StartTime, deleteRequest.EndTime); err != nil {
		level.Error(util_log.Logger).Log("msg", "error cancelling the delete request", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetCacheGenerationNumberHandler handles requests for a user's cache generation number
func (dm *DeleteRequestHandler) GetCacheGenerationNumberHandler(w http.ResponseWriter, r *http.Request) {
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

func query(params url.Values) (string, error) {
	query := params.Get("query")
	if len(query) == 0 {
		return "", errors.New("query not set")
	}

	if _, err := parseDeletionQuery(query); err != nil {
		return "", err
	}

	return query, nil
}

func startTime(params url.Values) (int64, error) {
	startParam := params.Get("start")
	if startParam == "" {
		return 0, errors.New("start time not set")
	}

	st, err := parseTime(startParam)
	if err != nil {
		return 0, errors.New("invalid start time: require unix seconds or RFC3339 format")
	}

	return st, nil
}

func endTime(params url.Values, startTime int64) (int64, error) {
	endParam := params.Get("end")
	endTime, err := parseTime(endParam)
	if err != nil {
		return 0, errors.New("invalid end time: require unix seconds or RFC3339 format")
	}

	if endTime > int64(model.Now()) {
		return 0, errors.New("deletes in the future are not allowed")
	}

	if startTime > endTime {
		return 0, errors.New("start time can't be greater than end time")
	}

	return endTime, nil
}

func parseTime(in string) (int64, error) {
	if in == "" {
		return int64(model.Now()), nil
	}

	t, err := time.Parse(time.RFC3339, in)
	if err != nil {
		return timeFromInt(in)
	}

	return t.UnixMilli(), nil
}

func timeFromInt(in string) (int64, error) {
	if len(in) != 10 {
		return 0, errors.New("not unix seconds")
	}

	return util.ParseTime(in)
}
