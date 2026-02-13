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

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestAddDeleteRequestHandler(t *testing.T) {
	t.Run("it adds the delete request to the store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001", false)

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.userID)
		require.Equal(t, `{foo="bar"}`, store.addReq.query)
		require.Equal(t, toTime("0000000000"), store.addReq.startTime)
		require.Equal(t, toTime("0000000001"), store.addReq.endTime)
	})

	t.Run("it only shards deletes with line filter based on a query param", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		now := model.Now()
		from := model.TimeFromUnix(now.Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(now.Unix())
		maxInterval := time.Hour

		req := buildRequest("org-id", `{foo="bar"} |= "foo"`, unixString(from), unixString(to), false)
		params := req.URL.Query()
		params.Set("max_interval", maxInterval.String())
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		require.Equal(t, maxInterval, store.addReq.shardByInterval)
	})

	t.Run("it uses the default for sharding when the query param isn't present", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, time.Hour, 0, nil)

		now := model.Now()
		from := model.TimeFromUnix(now.Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(now.Unix())

		req := buildRequest("org-id", `{foo="bar"} |= "foo"`, unixString(from), unixString(to), false)

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		require.Equal(t, time.Hour, store.addReq.shardByInterval)
	})

	t.Run("it does not shard deletes without line filter", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		from := model.TimeFromUnix(model.Now().Add(-3 * time.Hour).Unix())
		to := model.TimeFromUnix(from.Add(3 * time.Hour).Unix())

		req := buildRequest("org-id", `{foo="bar"}`, unixString(from), unixString(to), false)
		params := req.URL.Query()
		params.Set("max_interval", "1h")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)
		require.Equal(t, time.Duration(0), store.addReq.shardByInterval)
	})

	t.Run("it works with RFC3339", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "2006-01-02T15:04:05Z", "2006-01-03T15:04:05Z", false)

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.userID)
		require.Equal(t, `{foo="bar"}`, store.addReq.query)
		require.Equal(t, toTime("1136214245"), store.addReq.startTime)
		require.Equal(t, toTime("1136300645"), store.addReq.endTime)
	})

	t.Run("it fills in end time if blank", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "", false)

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.userID)
		require.Equal(t, `{foo="bar"}`, store.addReq.query)
		require.Equal(t, toTime("0000000000"), store.addReq.startTime)
		require.InDelta(t, int64(model.Now()), int64(store.addReq.endTime), 1000)
	})

	t.Run("it returns 500 when the delete store errors", func(t *testing.T) {
		store := &mockDeleteRequestsStore{addErr: errors.New("something bad")}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001", false)

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)
		require.Equal(t, w.Code, http.StatusInternalServerError)
	})

	t.Run("Validation", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, time.Minute, 0, nil)

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
			{"org-id", `{foo="bar"}`, "0000000001", "0000000000", "", "start time can't be greater than or equal to end time\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "not-a-duration", "invalid max_interval: valid time units are 's', 'm', 'h'\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "1ms", "invalid max_interval: valid time units are 's', 'm', 'h'\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "1h", "max_interval can't be greater than 1m0s\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000001", "30s", "max_interval can't be greater than the interval to be deleted (1s)\n"},
			{"org-id", `{foo="bar"} |= "foo"`, "0000000000", "0000000000", "", "start time can't be greater than or equal to end time\n"},
		} {
			t.Run(strings.TrimSpace(tc.error), func(t *testing.T) {
				req := buildRequest(tc.orgID, tc.query, tc.startTime, tc.endTime, false)

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

// buildEraseCompletedDeletionRequest builds a DELETE request for EraseCompletedDeletionRequestHandler.
// The handler only accepts start=1767762000 and end=1769576400.
func buildEraseCompletedDeletionRequest(orgID, query, start, end string) *http.Request {
	var req *http.Request
	if orgID == "" {
		req, _ = http.NewRequest(http.MethodDelete, "/loki/api/v1/cancelRequestWithoutID", nil)
	} else {
		ctx := user.InjectOrgID(context.Background(), orgID)
		req, _ = http.NewRequestWithContext(ctx, http.MethodDelete, "/loki/api/v1/cancelRequestWithoutID", nil)
	}
	q := req.URL.Query()
	q.Set("query", query)
	q.Set("start", start)
	q.Set("end", end)
	req.URL.RawQuery = q.Encode()
	return req
}

func TestEraseCompletedDeletionRequestHandler(t *testing.T) {
	const validStart = "1767762000"
	const validEnd = "1769576400"
	const validQuery = `{foo="bar"}`

	t.Run("successfully cancels a processed delete request matching query and time range", func(t *testing.T) {
		matchingReq := deletionproto.DeleteRequest{
			RequestID: "req-123",
			UserID:    "org-id",
			Query:     validQuery,
			StartTime: model.TimeFromUnix(1767762000),
			EndTime:   model.TimeFromUnix(1769576400),
			Status:    deletionproto.StatusProcessed,
		}
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{matchingReq}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusNoContent, w.Code)
		require.Equal(t, "org-id", store.getAllUser)
		require.False(t, store.getAllRequestedForQuerytimeFiltering)
		require.Equal(t, removeReqDetails{userID: "org-id", reqID: "req-123"}, store.removeReqs)
	})

	t.Run("retention not enabled when handler is nil", func(t *testing.T) {
		var h *DeleteRequestHandler
		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "Retention is not enabled\n", w.Body.String())
	})

	t.Run("no org id", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "no org id\n", w.Body.String())
	})

	t.Run("query not set", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", "", validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "query not set\n", w.Body.String())
	})

	t.Run("start time not set", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", validQuery, "", validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "start time not set\n", w.Body.String())
	})

	t.Run("start time must be 1767762000", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", validQuery, "1767762001", validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "start time should be 1767762000\n", w.Body.String())
	})

	t.Run("end time not set", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, "")
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "end")
	})

	t.Run("end time must be 1769576400", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, "1769576401")
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Equal(t, "end time should be 1769576400\n", w.Body.String())
	})

	t.Run("error getting delete requests from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllErr = errors.New("store unavailable")
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Equal(t, "store unavailable\n", w.Body.String())
	})

	t.Run("no matching delete request found", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{
			{Query: validQuery, StartTime: model.TimeFromUnix(1767762000), EndTime: model.TimeFromUnix(1769576399)},
		}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "No matching delete request found")
		require.Contains(t, w.Body.String(), validQuery)
	})

	t.Run("multiple matching delete requests found", func(t *testing.T) {
		matching := deletionproto.DeleteRequest{
			RequestID: "req-1",
			Query:     validQuery,
			StartTime: model.TimeFromUnix(1767762000),
			EndTime:   model.TimeFromUnix(1769576400),
			Status:    deletionproto.StatusProcessed,
		}
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{matching, matching}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "Multiple matching delete requests found")
		require.Contains(t, w.Body.String(), "Count: 2")
	})

	t.Run("delete request not in processed state", func(t *testing.T) {
		matchingReq := deletionproto.DeleteRequest{
			RequestID: "req-456",
			UserID:    "org-id",
			Query:     validQuery,
			StartTime: model.TimeFromUnix(1767762000),
			EndTime:   model.TimeFromUnix(1769576400),
			Status:    deletionproto.StatusReceived,
		}
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{matchingReq}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "is not in processed state")
		require.Contains(t, w.Body.String(), "req-456")
		require.Contains(t, w.Body.String(), "Cancellation of incomplete delete request is not allowed from this endpoint")
	})

	t.Run("error removing delete request from store", func(t *testing.T) {
		matchingReq := deletionproto.DeleteRequest{
			RequestID: "req-789",
			UserID:    "org-id",
			Query:     validQuery,
			StartTime: model.TimeFromUnix(1767762000),
			EndTime:   model.TimeFromUnix(1769576400),
			Status:    deletionproto.StatusProcessed,
		}
		store := &mockDeleteRequestsStore{}
		store.getAllResult = []deletionproto.DeleteRequest{matchingReq}
		store.removeErr = errors.New("remove failed")
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildEraseCompletedDeletionRequest("org-id", validQuery, validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.Equal(t, "remove failed\n", w.Body.String())
	})

	t.Run("invalid query format", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)
		req := buildEraseCompletedDeletionRequest("org-id", "not a logql query", validStart, validEnd)
		w := httptest.NewRecorder()
		h.EraseCompletedDeletionRequestHandler(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "query")
	})
}

func TestCancelDeleteRequestHandler(t *testing.T) {
	t.Run("it removes unprocessed delete requests from the store when force is true", func(t *testing.T) {
		stored := []deletionproto.DeleteRequest{
			{RequestID: "test-request", UserID: "org-id", Query: "test-query", SequenceNum: 1, Status: deletionproto.StatusReceived, CreatedAt: now.Add(-2 * time.Hour)},
		}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored

		h := NewDeleteRequestHandler(store, 0, time.Hour, nil)

		req := buildRequest("org-id", ``, "", "", false)
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		params.Set("force", "true")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, store.getUser, "org-id")
		require.Equal(t, store.getID, "test-request")
		require.Equal(t, removeReqDetails{
			userID: stored[0].UserID,
			reqID:  stored[0].RequestID,
		}, store.removeReqs)
	})

	t.Run("it returns an error when parts of the query have started to be processed", func(t *testing.T) {
		stored := []deletionproto.DeleteRequest{
			{RequestID: "test-request-1", CreatedAt: now, Status: deletionproto.StatusProcessed},
			{RequestID: "test-request-1", CreatedAt: now, Status: deletionproto.StatusReceived},
			{RequestID: "test-request-1", CreatedAt: now, Status: deletionproto.StatusProcessed},
		}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored

		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("org-id", ``, "", "", false)
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		params.Set("force", "false")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusBadRequest)
		require.Equal(t, "deletion of request which is in process or already processed is not allowed\n", w.Body.String())
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("orgid", ``, "", "", false)
		params := req.URL.Query()
		params.Set("request_id", "test-request")
		req.URL.RawQuery = params.Encode()

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("error removing from the store", func(t *testing.T) {
		stored := []deletionproto.DeleteRequest{{RequestID: "test-request", UserID: "org-id", Query: "test-query", Status: deletionproto.StatusReceived, CreatedAt: now}}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored
		store.removeErr = errors.New("something bad")

		h := NewDeleteRequestHandler(store, 0, time.Hour, nil)

		req := buildRequest("org-id", ``, "", "", false)
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
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)

			req := buildRequest("", ``, "", "", false)
			params := req.URL.Query()
			params.Set("request_id", "test-request")
			req.URL.RawQuery = params.Encode()

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})

		t.Run("request not found", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{getErr: ErrDeleteRequestNotFound}, 0, 0, nil)

			req := buildRequest("org-id", ``, "", "", false)
			params := req.URL.Query()
			params.Set("request_id", "test-request")
			req.URL.RawQuery = params.Encode()

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusNotFound)
			require.Equal(t, "could not find delete request with given id\n", w.Body.String())
		})

		t.Run("all requests in group are already processed", func(t *testing.T) {
			stored := []deletionproto.DeleteRequest{{RequestID: "test-request", UserID: "org-id", Query: "test-query", Status: deletionproto.StatusProcessed}}
			store := &mockDeleteRequestsStore{}
			store.getResult = stored

			h := NewDeleteRequestHandler(store, 0, 0, nil)

			req := buildRequest("org-id", ``, "", "", false)
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
		store.getAllResult = []deletionproto.DeleteRequest{{RequestID: "test-request-1", Status: deletionproto.StatusReceived}, {RequestID: "test-request-2", Status: deletionproto.StatusReceived}}
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		for _, forQuerytimeFiltering := range []bool{false, true} {
			req := buildRequest("org-id", ``, "", "", forQuerytimeFiltering)

			w := httptest.NewRecorder()
			h.GetAllDeleteRequestsHandler(w, req)

			require.Equal(t, w.Code, http.StatusOK)
			require.Equal(t, store.getAllUser, "org-id")

			var result []deletionproto.DeleteRequest
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.ElementsMatch(t, store.getAllResult, result)
			require.Equal(t, forQuerytimeFiltering, store.getAllRequestedForQuerytimeFiltering)
		}
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, 0, 0, nil)

		req := buildRequest("orgid", ``, "", "", false)
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
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, 0, 0, nil)

			req := buildRequest("", ``, "", "", false)

			w := httptest.NewRecorder()
			h.GetAllDeleteRequestsHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})
	})
}

func buildRequest(orgID, query, start, end string, forQuerytimeFiltering bool) *http.Request {
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
	if forQuerytimeFiltering {
		q.Set(ForQuerytimeFilteringQueryParam, "true")
	}

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

func verifyRequestSplits(t *testing.T, from, to model.Time, shardInterval time.Duration, reqs []deleteRequest) {
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
