package deletion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util"

	"github.com/weaveworks/common/user"
)

func TestAddDeleteRequestHandler(t *testing.T) {
	t.Run("it adds the delete request to the store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.UserID)
		require.Equal(t, `{foo="bar"}`, store.addReq.Query)
		require.Equal(t, toTime("0000000000"), store.addReq.StartTime)
		require.Equal(t, toTime("0000000001"), store.addReq.EndTime)
	})

	t.Run("it works with RFC3339", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "2006-01-02T15:04:05Z", "2006-01-03T15:04:05Z", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.UserID)
		require.Equal(t, `{foo="bar"}`, store.addReq.Query)
		require.Equal(t, toTime("1136214245"), store.addReq.StartTime)
		require.Equal(t, toTime("1136300645"), store.addReq.EndTime)
	})

	t.Run("it fills in end time if blank", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addReq.UserID)
		require.Equal(t, `{foo="bar"}`, store.addReq.Query)
		require.Equal(t, toTime("0000000000"), store.addReq.StartTime)
		require.InDelta(t, int64(model.Now()), int64(store.addReq.EndTime), 1000)
	})

	t.Run("it returns 500 when the delete store errors", func(t *testing.T) {
		store := &mockDeleteRequestsStore{addErr: errors.New("something bad")}
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)
		require.Equal(t, w.Code, http.StatusInternalServerError)
	})

	t.Run("Validation", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, nil)

		for _, tc := range []struct {
			orgID, query, startTime, endTime, error string
		}{
			{"", `{foo="bar"}`, "0000000000", "0000000001", "no org id\n"},
			{"org-id", "", "0000000000", "0000000001", "query not set\n"},
			{"org-id", `not a query`, "0000000000", "0000000001", "invalid query expression\n"},
			{"org-id", `{foo="bar"}`, "", "0000000001", "start time not set\n"},
			{"org-id", `{foo="bar"}`, "0000000000000", "0000000001", "invalid start time: require unix seconds or RFC3339 format\n"},
			{"org-id", `{foo="bar"}`, "0000000000", "0000000000001", "invalid end time: require unix seconds or RFC3339 format\n"},
			{"org-id", `{foo="bar"}`, "0000000000", fmt.Sprint(time.Now().Add(time.Hour).Unix())[:10], "deletes in the future are not allowed\n"},
			{"org-id", `{foo="bar"}`, "0000000001", "0000000000", "start time can't be greater than end time\n"},
		} {
			t.Run(strings.TrimSpace(tc.error), func(t *testing.T) {
				req := buildRequest(tc.orgID, tc.query, tc.startTime, tc.endTime, "")

				w := httptest.NewRecorder()
				h.AddDeleteRequestHandler(w, req)

				require.Equal(t, w.Code, http.StatusBadRequest)
				require.Equal(t, w.Body.String(), tc.error)
			})
		}
	})
}

func TestCancelDeleteRequestHandler(t *testing.T) {
	t.Run("it removes unprocessed delete requests from the store", func(t *testing.T) {
		stored := []DeleteRequest{
			{RequestID: "test-request", UserID: "org-id", Query: "test-query", SequenceNum: 0, Status: StatusProcessed},
			{RequestID: "test-request", UserID: "org-id", Query: "test-query", SequenceNum: 1, Status: StatusReceived},
		}
		store := &mockDeleteRequestsStore{}
		store.getResult = stored

		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", ``, "", "", "test-request")

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, store.getUser, "org-id")
		require.Equal(t, store.getID, "test-request")
		require.Equal(t, stored[1], store.removeReqs[0])
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org id", ``, "", "", "test-request")

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

		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", ``, "", "", "test-request")

		w := httptest.NewRecorder()
		h.CancelDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("Validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, nil)

			req := buildRequest("", ``, "", "", "test-request")

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})

		t.Run("request not found", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{getErr: ErrDeleteRequestNotFound}, nil)

			req := buildRequest("org-id", ``, "", "", "test-request")

			w := httptest.NewRecorder()
			h.CancelDeleteRequestHandler(w, req)

			require.Equal(t, w.Code, http.StatusNotFound)
			require.Equal(t, "could not find delete request with given id\n", w.Body.String())
		})

		t.Run("all requests in group are already processed", func(t *testing.T) {
			stored := []DeleteRequest{{RequestID: "test-request", UserID: "org-id", Query: "test-query", Status: StatusProcessed}}
			store := &mockDeleteRequestsStore{}
			store.getResult = stored

			h := NewDeleteRequestHandler(store, nil)

			req := buildRequest("org-id", ``, "", "", "test-request")

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
		store.getAllResult = []DeleteRequest{{RequestID: "test-request-1"}, {RequestID: "test-request-2"}}
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org-id", ``, "", "", "")

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusOK)
		require.Equal(t, store.getAllUser, "org-id")
		require.Equal(t, getAllResult, strings.TrimSpace(w.Body.String()))
	})

	t.Run("error getting from store", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		store.getAllErr = errors.New("something bad")
		h := NewDeleteRequestHandler(store, nil)

		req := buildRequest("org id", ``, "", "", "test-request")

		w := httptest.NewRecorder()
		h.GetAllDeleteRequestsHandler(w, req)

		require.Equal(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, "something bad\n", w.Body.String())
	})

	t.Run("validation", func(t *testing.T) {
		t.Run("no org id", func(t *testing.T) {
			h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, nil)

			req := buildRequest("", ``, "", "", "")

			w := httptest.NewRecorder()
			h.GetAllDeleteRequestsHandler(w, req)

			require.Equal(t, w.Code, http.StatusBadRequest)
			require.Equal(t, "no org id\n", w.Body.String())
		})
	})
}

func buildRequest(orgID, query, start, end, requestID string) *http.Request {
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
	q.Set("request_id", requestID)
	req.URL.RawQuery = q.Encode()

	return req
}

func toTime(t string) model.Time {
	modelTime, _ := util.ParseTime(t)
	return model.Time(modelTime)
}

var (
	getAllResult = `[{"request_id":"test-request-1","start_time":0,"end_time":0,"query":"","status":"","created_at":0},{"request_id":"test-request-2","start_time":0,"end_time":0,"query":"","status":"","created_at":0}]`
)
