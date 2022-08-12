package deletion

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
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
	deleteRequestsStore DeleteRequestsStore
	metrics             *deleteRequestHandlerMetrics
	maxQueryRange       time.Duration
}

// NewDeleteRequestHandler creates a DeleteRequestHandler
func NewDeleteRequestHandler(deleteStore DeleteRequestsStore, maxQueryRange time.Duration, registerer prometheus.Registerer) *DeleteRequestHandler {
	deleteMgr := DeleteRequestHandler{
		deleteRequestsStore: deleteStore,
		maxQueryRange:       maxQueryRange,
		metrics:             newDeleteRequestHandlerMetrics(registerer),
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

	queryRange, err := dm.queryRange(params, startTime, endTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deleteRequests := shardDeleteRequestsByQueryRange(startTime, endTime, query, userID, queryRange)
	if _, err := dm.deleteRequestsStore.AddDeleteRequestGroup(ctx, deleteRequests); err != nil {
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

func shardDeleteRequestsByQueryRange(startTime, endTime model.Time, query, userID string, queryRange time.Duration) []DeleteRequest {
	var deleteRequests []DeleteRequest
	for start := startTime; ; start = start.Add(queryRange) + 1 {
		if start.Equal(endTime) || start.After(endTime) {
			break
		}

		end := start.Add(queryRange)
		if end.After(endTime) {
			end = endTime
		}

		deleteRequests = append(deleteRequests,
			DeleteRequest{
				StartTime: start,
				EndTime:   end,
				Query:     query,
				UserID:    userID,
			})
	}
	return deleteRequests
}

func (dm *DeleteRequestHandler) queryRange(params url.Values, startTime, endTime model.Time) (time.Duration, error) {
	qr := params.Get("max_query_range")
	if qr == "" {
		if dm.maxQueryRange == 0 {
			return endTime.Sub(startTime), nil
		}

		return min(endTime.Sub(startTime), dm.maxQueryRange), nil
	}

	queryRange, err := time.ParseDuration(qr)
	if err != nil || queryRange < time.Second {
		return 0, errors.New("invalid query range: valid time units are 's', 'm', 'h'")
	}

	if queryRange > dm.maxQueryRange && dm.maxQueryRange != 0 {
		dur, err := time.ParseDuration(dm.maxQueryRange.String())
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "error parsing query range", "err", err)
			return 0, err
		}
		return 0, fmt.Errorf("query range can't be greater than %s", dur.String())
	}

	return queryRange, nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// GetAllDeleteRequestsHandler handles get all delete requests
func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deleteGroups, err := dm.deleteRequestsStore.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	deletesPerRequest := partitionByRequestID(deleteGroups)
	deleteRequests := mergeDeletes(deletesPerRequest)

	sort.Slice(deleteRequests, func(i, j int) bool {
		return deleteRequests[i].CreatedAt < deleteRequests[j].CreatedAt
	})

	if err := json.NewEncoder(w).Encode(deleteRequests); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
	}
}

func mergeDeletes(groups map[string][]DeleteRequest) []DeleteRequest {
	var mergedRequests []DeleteRequest
	for _, deletes := range groups {
		startTime, endTime := findStartAndEnd(deletes)
		newDelete := deletes[0]
		newDelete.StartTime = startTime
		newDelete.EndTime = endTime

		mergedRequests = append(mergedRequests, newDelete)
	}
	return mergedRequests
}

func findStartAndEnd(deletes []DeleteRequest) (model.Time, model.Time) {
	startTime := model.Time(math.MaxInt64)
	endTime := model.Time(0)

	for _, del := range deletes {
		if del.StartTime < startTime {
			startTime = del.StartTime
		}

		if del.EndTime > endTime {
			endTime = del.EndTime
		}
	}

	return startTime, endTime
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
	deleteRequests, err := dm.deleteRequestsStore.GetDeleteRequestGroup(ctx, userID, requestID)
	if err != nil {
		if errors.Is(err, ErrDeleteRequestNotFound) {
			http.Error(w, "could not find delete request with given id", http.StatusNotFound)
			return
		}

		level.Error(util_log.Logger).Log("msg", "error getting delete request from the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	toDelete := filterProcessed(deleteRequests)
	if len(toDelete) == 0 {
		http.Error(w, "deletion of request which is in process or already processed is not allowed", http.StatusBadRequest)
		return
	}

	if err := dm.deleteRequestsStore.RemoveDeleteRequests(ctx, toDelete); err != nil {
		level.Error(util_log.Logger).Log("msg", "error cancelling the delete request", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func filterProcessed(reqs []DeleteRequest) []DeleteRequest {
	var unprocessed []DeleteRequest
	for _, r := range reqs {
		if r.Status == StatusReceived {
			unprocessed = append(unprocessed, r)
		}
	}
	return unprocessed
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

func startTime(params url.Values) (model.Time, error) {
	startParam := params.Get("start")
	if startParam == "" {
		return 0, errors.New("start time not set")
	}

	st, err := parseTime(startParam)
	if err != nil {
		return 0, errors.New("invalid start time: require unix seconds or RFC3339 format")
	}

	return model.Time(st), nil
}

func endTime(params url.Values, startTime model.Time) (model.Time, error) {
	endParam := params.Get("end")
	endTime, err := parseTime(endParam)
	if err != nil {
		return 0, errors.New("invalid end time: require unix seconds or RFC3339 format")
	}

	if endTime > int64(model.Now()) {
		return 0, errors.New("deletes in the future are not allowed")
	}

	if int64(startTime) > endTime {
		return 0, errors.New("start time can't be greater than end time")
	}

	return model.Time(endTime), nil
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
