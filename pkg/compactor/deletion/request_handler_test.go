package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util"
)

func TestAddDeleteRequestHandler(t *testing.T) {
	t.Run("it adds the delete request to the store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReqs[0].UserID)
		require.Equal(t, `{foo="bar"}`, store.addReqs[0].Query)
		require.Equal(t, toTime("0000000000"), store.addReqs[0].StartTime)
		require.Equal(t, toTime("0000000001"), store.addReqs[0].EndTime)
	})

	t.Run("an error is returned if adding delete request group returned zero", func(t *testing.T) {
		store := &mockDeleteRequestsStore{returnZeroDeleteRequests: true}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
	})

	t.Run("it only shards deletes with line filter based on a query param", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, nil)

		now := model.Now()
		from := model.TimeFromUnix(now.Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(now.Unix())
		maxInterval := time.Hour

		req := buildRequest("org-id", `{foo="bar"} |= "foo"`, unixString(from), unixString(to))
		params := req.URL.Query()
		params.Set("max_interval", maxInterval.String())
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		verifyRequestSplits(t, from, to, maxInterval, store.addReqs)
	})

	t.Run("it uses the default for sharding when the query param isn't present", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, time.Hour, nil)

		now := model.Now()
		from := model.TimeFromUnix(now.Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(now.Unix())

		req := buildRequest("org-id", `{foo="bar"} |= "foo"`, unixString(from), unixString(to))

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		verifyRequestSplits(t, from, to, time.Hour, store.addReqs)
	})

	t.Run("it does not shard deletes without line filter", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, nil)

		from := model.TimeFromUnix(model.Now().Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(from.Add(3 * time.Hour).Unix())

		req := buildRequest("org-id", `{foo="bar"}`, unixString(from), unixString(to))
		params := req.URL.Query()
		params.Set("max_interval", "1h")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		require.Len(t, store.addReqs, 1)
		require.Equal(t, from, store.addReqs[0].StartTime)
		require.Equal(t, to, store.addReqs[0].EndTime)
	})

	t.Run("it works with RFC3339", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "2006-01-02T15:04:05Z", "2006-01-03T15:04:05Z")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReqs[0].UserID)
		require.Equal(t, `{foo="bar"}`, store.addReqs[0].Query)
		require.Equal(t, toTime("1136214245"), store.addReqs[0].StartTime)
		require.Equal(t, toTime("1136300645"), store.addReqs[0].EndTime)
	})

	t.Run("it fills in end time if blank", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReqs[0].UserID)
		require.Equal(t, `{foo="bar"}`, store.addReqs[0].Query)
		require.Equal(t, toTime("0000000000"), store.addReqs[0].StartTime)
		require.InDelta(t, int64(model.Now()), int64(store.addReqs[0].EndTime), 1000)
	})

	t.Run("it returns 500 when the delete store errors", func(t *testing.T) {
		store := &mockDeleteRequestsStore{addErr: errors.New("something bad")}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)
		require.Equal(t, w.Code, http.StatusInternalServerError)
	})

	t.Run("Validation", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, time.Minute, nil)

		for _, tc := range []struct {
			orgID, query, startTime, endTime, interval, error string
		}{
			{"", `{foo="bar"}`, "0000000000", "0000000001", "", "no org id\n"},
			{"org-id", "", "0000000000", "0000000001", "", "query not set\n"},
			{"org-id", `not a query`, "0000000000", "0000000001", "", "invalid query expression\n"},
			{"org-id", `{foo="bar"}`, "", "0000000001", "", "start time not set\n"},
			{"org-id", `{foo="bar"}`, "0000000000000", "0000000001", "", "invalid start time: require unix seconds or RFC3339 format\n"},
			{"org-id", `{foo="bar"}`, "0000000000", "0000000000001", "", "invalid end time: require unix seconds or RFC3339 format\n"},
			{"org-id", `{foo="bar"}`, "0000000000", fmt.Sprint(time.Now().Add(time.Hour).Unix())[:10], "", "deletes in the future are not allowed\n"},
			{"org-id", `{foo="bar"}`, "0000000001", "0000000000", "", "start time can't be greater than end time\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "not-a-duration", "invalid max_interval: valid time units are 's', 'm', 'h'\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "1ms", "invalid max_interval: valid time units are 's', 'm', 'h'\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "1h", "max_interval can't be greater than 1m0s\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "30s", "max_interval can't be greater than the interval to be deleted (1s)\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000000", "", "difference between start time and end time must be at least one second\n"},
		} {
			t.Run(strings.TrimSpace(tc.error), func(t *testing.T) {
				req := buildRequest(tc.orgID, tc.query, tc.startTime, tc.endTime)

				params := req.URL.Query()
				params.Set("max_interval", tc.interval)
				req.URL.RawQuery = params.Encode()

				w := httptest.NewRecorder()
				h.AddDeleteRequestHandler(w, req)

				require.Equal(t, w.Code, http.StatusBadRequest)
				require.Equal(t, w.Body.String(), tc.error)
			})
		}
	})
}

func TestCancelDeleteRequestHandler(t *testing.T) {
	t.Run("it removes unprocessed delete requests from the store when force is true", func(t *testing.T) {
		stored := []DeleteRequest{
			{RequestID: "test-request", UserID: "org-id", Query: "test-query", SequenceNum: 0, Status: StatusProcessed},
			{RequestID: "test-request", UserID: "org-id", Query: "test-query", SequenceNum: 1, Status: StatusReceived},
		}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored

		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		params.Set("force", "true")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, store.getUser, "org-id")
		require.Equal(t, store.getID, "test-request")
		require.Equal(t, stored[1], store.removeReqs[0])
	})

	t.Run("it returns an error when parts of the query have started to be processed", func(t *testing.T) {
		stored := []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusReceived},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
		}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored

		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		params.Set("force", "false")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusBadRequest)
		require.Equal(t, "Unable to cancel partially completed delete request. To force, use the ?force query parameter\n", w.Body.String())
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("orgid", ``, "", "")
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("error removing from the store", func(t *testing.T) {
		stored := []DeleteRequest{{RequestID: "test-request", UserID: "org-id", Query: "test-query", Status: StatusReceived}}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored
		store.removeErr = errors.New("something bad")

		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("Validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, nil)

			req := buildRequest("", ``, "", "")
			params := req.URL.Query()
			params.Set("request_id", "test-request")
			req.URL.RawQuery = params.Encode()

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})

		t.Run("request not found", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{getErr: ErrDeleteRequestNotFound}, 0, nil)

			req := buildRequest("org-id", ``, "", "")
			params := req.URL.Query()
			params.Set("request_id", "test-request")
			req.URL.RawQuery = params.Encode()

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusNotFound)
			require.Equal(t, "could not find delete request with given id\n", w.Body.String())
		})

		t.Run("all requests in group are already processed", func(t *testing.T) {
			stored := []DeleteRequest{{RequestID: "test-request", UserID: "org-id", Query: "test-query", Status: StatusProcessed}}
			store := &mockDeleteRequestsStore{}
			store.getResult = stored

			h := NewDeleteRequestHandler(store, 0, nil)

			req := buildRequest("org-id", ``, "", "")
			params := req.URL.Query()
			params.Set("request_id", "test-request")
			req.URL.RawQuery = params.Encode()

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "deletion of request which is in process or already processed is not allowed\n", w.Body.String())
		})
	})
}

func TestGetAllDeleteRequestsHandler(t *testing.T) {
	t.Run("it gets all the delete requests for the user", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{{RequestID: "test-request-1", Status: StatusReceived}, {RequestID: "test-request-2", Status: StatusReceived}}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusOK)
		require.Equal(t, store.getAllUser, "org-id")

		var result []DeleteRequest
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.ElementsMatch(t, store.getAllResult, result)
	})

	t.Run("it merges requests with the same requestID", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now, EndTime: now.Add(time.Hour)},
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now.Add(2 * time.Hour), EndTime: now.Add(3 * time.Hour)},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), StartTime: now.Add(30 * time.Minute), EndTime: now.Add(90 * time.Minute)},
			{RequestID: "test-request-1", CreatedAt: now, StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour)},
		}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusOK)

		var result []DeleteRequest
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

		require.Len(t, result, 2)
		require.Equal(t, []DeleteRequest{
			{RequestID: "test-request-1", Status: StatusReceived, CreatedAt: now, StartTime: now, EndTime: now.Add(3 * time.Hour)},
			{RequestID: "test-request-2", Status: StatusReceived, CreatedAt: now.Add(time.Minute), StartTime: now.Add(30 * time.Minute), EndTime: now.Add(90 * time.Minute)},
		}, result)
	})

	t.Run("it only considers a request processed if all it's subqueries are processed", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusReceived},
			{RequestID: "test-request-1", CreatedAt: now, Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-3", CreatedAt: now.Add(2 * time.Minute), Status: StatusReceived},
		}
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("org-id", ``, "", "")

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusOK)

		var result []DeleteRequest
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

		require.Len(t, result, 3)
		require.Equal(t, []DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: "66% Complete"},
			{RequestID: "test-request-2", CreatedAt: now.Add(time.Minute), Status: StatusProcessed},
			{RequestID: "test-request-3", CreatedAt: now.Add(2 * time.Minute), Status: StatusReceived},
		}, result)
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, 0, nil)

		req := buildRequest("orgid", ``, "", "")
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, nil)

			req := buildRequest("", ``, "", "")

			w := httptest.NewRecorder()
			h.GetAllDeleteRequestsHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})
	})
}

func buildRequest(orgID, query, start, end string) *http.Request {
	var req *http.Request
	if orgID == "" {
		req, _ = http.NewRequest(http.MethodGet, "", nil)
	} else {
		ctx := user.InjectOrgID(context.Background(), orgID)
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	}

	q := req.URL.Query()
	q.Set("query", query)
	q.Set("start", start)
	q.Set("end", end)

	req.URL.RawQuery = q.Encode()

	return req
}

func unixString(t model.Time) string {
	return fmt.Sprint(t.Unix())
}

func toTime(t string) model.Time {
	modelTime, _ := util.ParseTime(t)
	return model.Time(modelTime)
}

func verifyRequestSplits(t *testing.T, from, to model.Time, shardInterval time.Duration, reqs []DeleteRequest) {
	numExpectedRequests := 3
	shardAlignedStart := model.TimeFromUnixNano(time.Unix(0, from.UnixNano()-from.UnixNano()%shardInterval.Nanoseconds()).UnixNano())
	if !from.Equal(shardAlignedStart) {
		numExpectedRequests++
	}

	require.Len(t, reqs, numExpectedRequests)
	for i := 0; i < numExpectedRequests; i++ {
		if i == 0 {
			// start of first request should be same as the start time in original request
			require.Equal(t, from, reqs[i].StartTime)
			// end of first request should be shard interval aligned start + shardInterval
			expectedEnd := shardAlignedStart.Add(shardInterval)
			if expectedEnd.After(to) {
				expectedEnd = to
			}
			require.Equal(t, expectedEnd, reqs[i].EndTime)
		} else {
			// start of this request should be equal to end of last request
			require.Equal(t, reqs[i-1].EndTime, reqs[i].StartTime)
			// if this is not last request then end of this split should be start + interval
			// if this is last request then end should be equal end of original request
			expectedEnd := reqs[i].StartTime.Add(shardInterval)
			if i == numExpectedRequests-1 {
				expectedEnd = to
			}
			require.Equal(t, expectedEnd, reqs[i].EndTime)
		}
	}
}
