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

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// DeleteRequestHandler provides handlers for delete requests
type DeleteRequestHandler struct {
	deleteRequestsStore DeleteRequestsStore
	metrics             *deleteRequestHandlerMetrics
	maxInterval         time.Duration
}

// NewDeleteRequestHandler creates a DeleteRequestHandler
func NewDeleteRequestHandler(deleteStore DeleteRequestsStore, maxInterval time.Duration, registerer prometheus.Registerer) *DeleteRequestHandler {
	deleteMgr := DeleteRequestHandler{
		deleteRequestsStore: deleteStore,
		maxInterval:         maxInterval,
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
	query, parsedExpr, err := query(params)
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

	var shardByInterval time.Duration
	var deleteRequests []DeleteRequest
	// shard delete requests only when there are line filters
	if parsedExpr.HasFilter() {
		var err error
		shardByInterval, err = dm.interval(params, startTime, endTime)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if shardByInterval == 0 || shardByInterval >= endTime.Sub(startTime) {
		shardByInterval = 0
		deleteRequests = []DeleteRequest{
			{
				StartTime: startTime,
				EndTime:   endTime,
				Query:     query,
				UserID:    userID,
			},
		}
	} else {
		deleteRequests = make([]DeleteRequest, 0, endTime.Sub(startTime)/shardByInterval)
		// although delete request end time is inclusive, setting endTimeInclusive to true would keep 1ms gap between the splits,
		// which might make us miss deletion of some of the logs. We set it to false to have some overlap between the request to stay safe.
		util.ForInterval(shardByInterval, startTime.Time(), endTime.Time(), false, func(start, end time.Time) {
			deleteRequests = append(deleteRequests,
				DeleteRequest{
					StartTime: model.Time(start.UnixMilli()),
					EndTime:   model.Time(end.UnixMilli()),
					Query:     query,
					UserID:    userID,
				},
			)
		})
	}

	createdDeleteRequests, err := dm.deleteRequestsStore.AddDeleteRequestGroup(ctx, deleteRequests)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error adding delete request to the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(createdDeleteRequests) == 0 {
		level.Error(util_log.Logger).Log("msg", "zero delete requests created", "user", userID, "query", query)
		http.Error(w, "Zero delete requests were created due to an internal error. Please contact support.", http.StatusInternalServerError)
		return
	}

	level.Info(util_log.Logger).Log(
		"msg", "delete request for user added",
		"delete_request_id", createdDeleteRequests[0].RequestID,
		"user", userID,
		"query", query,
		"interval", shardByInterval.String(),
	)

	dm.metrics.deleteRequestsReceivedTotal.WithLabelValues(userID).Inc()
	w.WriteHeader(http.StatusNoContent)
}

func (dm *DeleteRequestHandler) interval(params url.Values, startTime, endTime model.Time) (time.Duration, error) {
	qr := params.Get("max_interval")
	if qr == "" {
		return dm.intervalFromStartAndEnd(startTime, endTime)
	}

	interval, err := time.ParseDuration(qr)
	if err != nil || interval < time.Second {
		return 0, errors.New("invalid max_interval: valid time units are 's', 'm', 'h'")
	}

	if interval > dm.maxInterval && dm.maxInterval != 0 {
		return 0, fmt.Errorf("max_interval can't be greater than %s", dm.maxInterval.String())
	}

	if interval > endTime.Sub(startTime) {
		return 0, fmt.Errorf("max_interval can't be greater than the interval to be deleted (%s)", endTime.Sub(startTime))
	}

	return interval, nil
}

func (dm *DeleteRequestHandler) intervalFromStartAndEnd(startTime, endTime model.Time) (time.Duration, error) {
	interval := endTime.Sub(startTime)
	if interval < time.Second {
		return 0, errors.New("difference between start time and end time must be at least one second")
	}

	if dm.maxInterval == 0 {
		return interval, nil
	}
	return min(interval, dm.maxInterval), nil
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(deleteRequests); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
	}
}

func mergeDeletes(groups map[string][]DeleteRequest) []DeleteRequest {
	mergedRequests := []DeleteRequest{} // Declare this way so the return value is [] rather than null
	for _, deletes := range groups {
		startTime, endTime, status := mergeData(deletes)
		newDelete := deletes[0]
		newDelete.StartTime = startTime
		newDelete.EndTime = endTime
		newDelete.Status = status

		mergedRequests = append(mergedRequests, newDelete)
	}
	return mergedRequests
}

func mergeData(deletes []DeleteRequest) (model.Time, model.Time, DeleteRequestStatus) {
	var (
		startTime    = model.Time(math.MaxInt64)
		endTime      = model.Time(0)
		numProcessed = 0
	)

	for _, del := range deletes {
		if del.StartTime < startTime {
			startTime = del.StartTime
		}

		if del.EndTime > endTime {
			endTime = del.EndTime
		}

		if del.Status == StatusProcessed {
			numProcessed++
		}
	}

	return startTime, endTime, deleteRequestStatus(numProcessed, len(deletes))
}

func deleteRequestStatus(processed, total int) DeleteRequestStatus {
	if processed == 0 {
		return StatusReceived
	}

	if processed == total {
		return StatusProcessed
	}

	percentCompleted := float64(processed) / float64(total)
	return DeleteRequestStatus(fmt.Sprintf("%d%% Complete", int(percentCompleted*100)))
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

	if len(toDelete) != len(deleteRequests) && params.Get("force") != "true" {
		http.Error(w, "Unable to cancel partially completed delete request. To force, use the ?force query parameter", http.StatusBadRequest)
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

func query(params url.Values) (string, syntax.LogSelectorExpr, error) {
	query := params.Get("query")
	if len(query) == 0 {
		return "", nil, errors.New("query not set")
	}

	parsedExpr, err := parseDeletionQuery(query)
	if err != nil {
		return "", nil, err
	}

	return query, parsedExpr, nil
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
