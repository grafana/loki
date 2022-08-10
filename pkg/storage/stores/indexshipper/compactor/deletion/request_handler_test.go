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
		h := NewDeleteRequestHandler(store, time.Second, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addedUser)
		require.Equal(t, `{foo="bar"}`, store.addedQuery)
		require.Equal(t, toTime("0000000000"), store.addedStartTime)
		require.Equal(t, toTime("0000000001"), store.addedEndTime)
	})

	t.Run("it works with RFC3339", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, time.Second, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "2006-01-02T15:04:05Z", "2006-01-03T15:04:05Z")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addedUser)
		require.Equal(t, `{foo="bar"}`, store.addedQuery)
		require.Equal(t, toTime("1136214245"), store.addedStartTime)
		require.Equal(t, toTime("1136300645"), store.addedEndTime)
	})

	t.Run("it fills in end time if blank", func(t *testing.T) {
		store := &mockDeleteRequestsStore{}
		h := NewDeleteRequestHandler(store, time.Second, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)

		require.Equal(t, w.Code, http.StatusNoContent)

		require.Equal(t, "org-id", store.addedUser)
		require.Equal(t, `{foo="bar"}`, store.addedQuery)
		require.Equal(t, toTime("0000000000"), store.addedStartTime)
		require.InDelta(t, int64(model.Now()), int64(store.addedEndTime), 1000)
	})

	t.Run("it returns 500 when the delete store errors", func(t *testing.T) {
		store := &mockDeleteRequestsStore{addErr: errors.New("something bad")}
		h := NewDeleteRequestHandler(store, time.Second, nil)

		req := buildRequest("org-id", `{foo="bar"}`, "0000000000", "0000000001")

		w := httptest.NewRecorder()
		h.AddDeleteRequestHandler(w, req)
		require.Equal(t, w.Code, http.StatusInternalServerError)
	})

	t.Run("Validation", func(t *testing.T) {
		h := NewDeleteRequestHandler(&mockDeleteRequestsStore{}, time.Second, nil)

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
				req := buildRequest(tc.orgID, tc.query, tc.startTime, tc.endTime)

				w := httptest.NewRecorder()
				h.AddDeleteRequestHandler(w, req)

				require.Equal(t, w.Code, http.StatusBadRequest)
				require.Equal(t, w.Body.String(), tc.error)
			})
		}
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

func toTime(t string) model.Time {
	modelTime, _ := util.ParseTime(t)
	return model.Time(modelTime)
}
