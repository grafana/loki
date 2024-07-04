package querier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestInstantQueryHandler(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	t.Run("log selector expression not allowed for instant queries", func(t *testing.T) {
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		ctx := user.InjectOrgID(context.Background(), "user")
		req, err := http.NewRequestWithContext(ctx, "GET", `/api/v1/query`, nil)
		require.NoError(t, err)

		q := req.URL.Query()
		q.Add("query", `{app="loki"}`)
		req.URL.RawQuery = q.Encode()
		err = req.ParseForm()
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		handler := NewQuerierHandler(api)
		httpHandler := NewQuerierHTTPHandler(handler)

		httpHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, logqlmodel.ErrUnsupportedSyntaxForInstantQuery.Error(), rr.Body.String())
	})
}

func TestTailHandler(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

	req, err := http.NewRequest("GET", `/`, nil)
	require.NoError(t, err)
	q := req.URL.Query()
	q.Add("query", `{app="loki"}`)
	req.URL.RawQuery = q.Encode()
	err = req.ParseForm()
	require.NoError(t, err)

	ctx := user.InjectOrgID(req.Context(), "1|2")
	req = req.WithContext(ctx)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.TailHandler)

	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Equal(t, "multiple org IDs present", rr.Body.String())
}

type slowConnectionSimulator struct {
	sleepFor   time.Duration
	deadline   time.Duration
	didTimeout bool
}

func (s *slowConnectionSimulator) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		panic(fmt.Sprintf("context already errored: %s", err))

	}
	time.Sleep(s.sleepFor)

	select {
	case <-ctx.Done():
		switch ctx.Err() {
		case context.DeadlineExceeded:
			s.didTimeout = true
		case context.Canceled:
			panic("context already canceled")
		}
	case <-time.After(s.deadline):
	}
}

func TestQueryWrapperMiddleware(t *testing.T) {
	shortestTimeout := time.Millisecond * 5

	t.Run("request timeout is the shortest one", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(time.Millisecond * 10)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)

		// request timeout is 5ms but it sleeps for 100ms, so timeout injected in the request is expected.
		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", limits).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), shortestTimeout)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		default:
			require.FailNow(t, "timeout expected")
		}

		require.True(t, connSimulator.didTimeout)
	})

	t.Run("apply limits query timeout", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)

		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", limits).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), time.Millisecond*100)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		}

		require.True(t, connSimulator.didTimeout)
	})
}

func TestSeriesHandler(t *testing.T) {
	t.Run("instant queries set a step of 0", func(t *testing.T) {
		ret := func() *logproto.SeriesResponse {
			return &logproto.SeriesResponse{
				Series: []logproto.SeriesIdentifier{
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "a", Value: "1"},
							{Key: "b", Value: "2"},
						},
					},
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "c", Value: "3"},
							{Key: "d", Value: "4"},
						},
					},
				},
			}
		}
		expected := `{"status":"success","data":[{"a":"1","b":"2"},{"c":"3","d":"4"}]}`

		q := newQuerierMock()
		q.On("Series", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(q)
		handler := NewQuerierHTTPHandler(NewQuerierHandler(api))

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/series"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		res := makeRequest(t, handler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})
}
func TestVolumeHandler(t *testing.T) {
	ret := &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
	}

	t.Run("shared beavhior between range and instant queries", func(t *testing.T) {
		for _, tc := range []struct {
			mode string
			req  *logproto.VolumeRequest
		}{
			{mode: "instant", req: loghttp.NewVolumeInstantQueryWithDefaults(`{foo="bar"}`)},
			{mode: "range", req: loghttp.NewVolumeRangeQueryWithDefaults(`{foo="bar"}`)},
		} {
			t.Run(fmt.Sprintf("%s queries return label volumes from the querier", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(ret, nil)
				api := setupAPI(querier)

				res, err := api.VolumeHandler(context.Background(), tc.req)
				require.NoError(t, err)

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)

				request := calls[0].Arguments[1].(*logproto.VolumeRequest)
				require.Equal(t, `{foo="bar"}`, request.Matchers)
				require.Equal(t, "series", request.AggregateBy)

				require.Equal(t, ret, res)
			})

			t.Run(fmt.Sprintf("%s queries return nothing when a store doesn't support label volumes", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(nil, nil)
				api := setupAPI(querier)

				res, err := api.VolumeHandler(context.Background(), tc.req)
				require.NoError(t, err)

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)

				require.Empty(t, res.Volumes)
			})

			t.Run(fmt.Sprintf("%s queries return error when there's an error in the querier", tc.mode), func(t *testing.T) {
				err := errors.New("something bad")
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(nil, err)

				api := setupAPI(querier)

				_, err = api.VolumeHandler(context.Background(), tc.req)
				require.ErrorContains(t, err, "something bad")

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)
			})
		}
	})
}

func makeRequest(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	err := req.ParseForm()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func setupAPI(querier *querierMock) *QuerierAPI {
	api := NewQuerierAPI(Config{}, querier, nil, log.NewNopLogger())
	return api
}
