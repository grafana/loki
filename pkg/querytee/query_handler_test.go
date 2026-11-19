package querytee

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestQueryHandler(t *testing.T) {
	const query = `{app="test"}`

	newRequest := func(t *testing.T, tenantID string) *http.Request {
		t.Helper()

		ctx := user.InjectOrgID(t.Context(), tenantID)
		httpReq, err := queryrange.DefaultCodec.EncodeRequest(ctx, &queryrange.LokiRequest{
			Query:   query,
			StartTs: time.Now().Add(-time.Hour),
			EndTs:   time.Now(),
			Step:    60000,
			Limit:   100,
			Path:    constants.PathLokiQueryRange,
		})
		require.NoError(t, err)
		return httpReq
	}

	type fanOutCall struct {
		query    string
		tenantID string
	}

	var calls []fanOutCall
	fanOutHandler := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		tenantID, err := user.ExtractOrgID(ctx)
		require.NoError(t, err)
		calls = append(calls, fanOutCall{query: req.GetQuery(), tenantID: tenantID})
		return &queryrange.LokiResponse{Status: "success", Data: queryrange.LokiData{ResultType: "streams"}}, nil
	})

	serve := func(handler queryrangebase.Handler, httpReq *http.Request) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		NewQueryHandler(queryrange.DefaultCodec, handler, log.NewNopLogger()).ServeHTTP(recorder, httpReq)
		return recorder
	}

	t.Run("sends the decoded query and the tenant ID to the fan-out handler", func(t *testing.T) {
		calls = nil

		recorder := serve(fanOutHandler, newRequest(t, "test-tenant"))

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, []fanOutCall{{query: query, tenantID: "test-tenant"}}, calls)
	})

	t.Run("sends a multi-tenant request to the fan-out handler", func(t *testing.T) {
		calls = nil

		recorder := serve(fanOutHandler, newRequest(t, "tenant1|tenant2"))

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, []fanOutCall{{query: query, tenantID: "tenant1|tenant2"}}, calls)
	})

	t.Run("writes the status and the body of a backend error", func(t *testing.T) {
		backendError := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
			return &NonDecodableResponse{StatusCode: http.StatusBadRequest, Body: []byte("parse error")}, errors.New("backend failed")
		})

		recorder := serve(backendError, newRequest(t, "test-tenant"))

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, "parse error\n", recorder.Body.String())
	})

	t.Run("answers 401 without calling the fan-out handler when the request has no tenant ID", func(t *testing.T) {
		calls = nil
		httpReq := newRequest(t, "test-tenant")
		httpReq.Header.Del(user.OrgIDHeaderName)

		recorder := serve(fanOutHandler, httpReq)

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Equal(t, user.ErrNoOrgID.Error()+"\n", recorder.Body.String())
		require.Empty(t, calls)
	})

	t.Run("answers 400 without calling the fan-out handler when the request cannot be decoded", func(t *testing.T) {
		calls = nil
		httpReq := newRequest(t, "test-tenant")
		q := httpReq.URL.Query()
		q.Set("start", "not-a-time")
		httpReq.URL.RawQuery = q.Encode()

		recorder := serve(fanOutHandler, httpReq)

		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Equal(t, `could not parse 'start' parameter: strconv.ParseInt: parsing "not-a-time": invalid syntax`, recorder.Body.String())
		require.Empty(t, calls)
	})
}
