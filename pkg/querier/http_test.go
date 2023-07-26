package querier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/validation"
)

func TestTailHandler(t *testing.T) {
	tenant.WithDefaultResolver(tenant.NewMultiResolver())

	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

	req, err := http.NewRequest("GET", "/", nil)
	ctx := user.InjectOrgID(req.Context(), "1|2")
	req = req.WithContext(ctx)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.TailHandler)

	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Equal(t, "multiple org IDs present\n", rr.Body.String())
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
	tenant.WithDefaultResolver(tenant.NewMultiResolver())
	shortestTimeout := time.Millisecond * 5

	t.Run("request timeout is the shortest one", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		// request timeout is 5ms but it sleeps for 100ms, so timeout injected in the request is expected.
		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		api.cfg.QueryTimeout = time.Millisecond * 10
		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

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

	t.Run("old querier:query_timeout is configured to supersede all others", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)
		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		// configure old querier:query_timeout parameter.
		// although it is longer than the limits timeout, it should supersede it.
		api.cfg.QueryTimeout = time.Millisecond * 100

		// although limits:query_timeout is shorter than querier:query_timeout,
		// limits:query_timeout should be ignored.
		// here we configure it to sleep for 100ms and we want it to timeout at the 100ms.
		connSimulator := &slowConnectionSimulator{
			sleepFor: api.cfg.QueryTimeout,
			deadline: time.Millisecond * 200,
		}

		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), time.Millisecond*200)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			require.FailNow(t, fmt.Sprintf("should timeout in %s", api.cfg.QueryTimeout))
		case <-time.After(shortestTimeout):
			// didn't use the limits timeout (i.e: shortest one), exactly what we want.
			break
		case <-time.After(api.cfg.QueryTimeout):
			require.FailNow(t, fmt.Sprintf("should timeout in %s", api.cfg.QueryTimeout))
		}

		require.True(t, connSimulator.didTimeout)
	})

	t.Run("new limits query timeout is configured to supersede all others", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

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

func TestSeriesVolumeHandler(t *testing.T) {
	ret := &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
	}

	setupAPI := func(querier *querierMock) *QuerierAPI {
		return NewQuerierAPI(Config{}, querier, nil, log.NewNopLogger())
	}

	makeRequest := func(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
		err := req.ParseForm()
		require.NoError(t, err)

		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}

	t.Run("shared beavhior between range and instant queries", func(t *testing.T) {
		for _, tc := range []struct {
			mode    string
			handler func(api *QuerierAPI) http.HandlerFunc
		}{
			{mode: "instant", handler: func(api *QuerierAPI) http.HandlerFunc { return api.SeriesVolumeInstantHandler }},
			{mode: "range", handler: func(api *QuerierAPI) http.HandlerFunc { return api.SeriesVolumeRangeHandler }},
		} {
			t.Run(fmt.Sprintf("%s queries return label volumes from the querier", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume"+
					"?start=0"+
					"&end=1"+
					"&query=%7Bfoo%3D%22bar%22%7D", nil)

				w := makeRequest(tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				request := calls[0].Arguments[1].(*logproto.VolumeRequest)
				require.Equal(t, `{foo="bar"}`, request.Matchers)

				require.Equal(
					t,
					`{"volumes":[{"name":"{foo=\"bar\"}","volume":38}]}`,
					strings.TrimSpace(w.Body.String()),
				)
				require.Equal(t, http.StatusOK, w.Result().StatusCode)
			})

			t.Run(fmt.Sprintf("%s queries return nothing when a store doesn't support label volumes", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(nil, nil)
				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
				w := makeRequest(tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				require.Equal(t, strings.TrimSpace(w.Body.String()), `{"volumes":[]}`)
				require.Equal(t, http.StatusOK, w.Result().StatusCode)
			})

			t.Run(fmt.Sprintf("%s queries return error when there's an error in the querier", tc.mode), func(t *testing.T) {
				err := errors.New("something bad")
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(nil, err)

				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
				w := makeRequest(tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				require.Equal(t, strings.TrimSpace(w.Body.String()), `something bad`)
				require.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
			})
		}
	})

	t.Run("instant queries set a step of 0", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(api.SeriesVolumeInstantHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, int64(0), request.Step)
	})

	t.Run("range queries parse step from request", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(api.SeriesVolumeRangeHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, (42 * time.Second).Milliseconds(), request.Step)
	})

	t.Run("range queries provide default step when not provided", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(api.SeriesVolumeRangeHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, time.Second.Milliseconds(), request.Step)
	})
}
