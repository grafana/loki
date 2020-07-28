package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
)

var (
	testTime   = time.Date(2019, 12, 02, 11, 10, 10, 10, time.UTC)
	testConfig = Config{queryrange.Config{
		SplitQueriesByInterval: 4 * time.Hour,
		AlignQueriesWithStep:   true,
		MaxRetries:             3,
		CacheResults:           true,
		ResultsCacheConfig: queryrange.ResultsCacheConfig{
			LegacyMaxCacheFreshness: 1 * time.Minute,
			CacheConfig: cache.Config{
				EnableFifoCache: true,
				Fifocache: cache.FifoCacheConfig{
					MaxSizeItems: 1024,
					Validity:     24 * time.Hour,
				},
			},
		},
	}}
	matrix = promql.Matrix{
		{
			Points: []promql.Point{
				{
					T: toMs(testTime.Add(-4 * time.Hour)),
					V: 0.013333333333333334,
				},
			},
			Metric: []labels.Label{
				{
					Name:  "filename",
					Value: `/var/hostlog/apport.log`,
				},
				{
					Name:  "job",
					Value: "varlogs",
				},
			},
		},
	}
	streams = logql.Streams{
		{
			Entries: []logproto.Entry{
				{Timestamp: testTime.Add(-4 * time.Hour), Line: "foo"},
				{Timestamp: testTime.Add(-1 * time.Hour), Line: "barr"},
			},
			Labels: `{filename="/var/hostlog/apport.log", job="varlogs"}`,
		},
	}

	seriesIdentifierData = []logproto.SeriesIdentifier{
		{
			Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
		},
		{
			Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
		},
		{
			Labels: map[string]string{"filename": "xyz.log", "job": "varlogs"},
		},
		{
			Labels: map[string]string{"filename": "/var/hostlog/other.log", "job": "varlogs"},
		},
	}
	series = []mockSeries{
		{
			testTime.Add(-6 * time.Hour),
			testTime.Add(-2 * time.Hour),
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[0],
				seriesIdentifierData[1],
			},
		},
		{
			testTime.Add(-1 * time.Hour),
			testTime,
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[2],
			},
		},
		{
			testTime.Add(1 * time.Hour),
			testTime.Add(6 * time.Hour),
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[3],
			},
		},
	}
)

// those tests are mostly for testing the glue between all component and make sure they activate correctly.
func TestMetricsTripperware(t *testing.T) {

	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	lreq := &LokiRequest{
		Query:     `rate({app="foo"} |= "foo"[1m])`,
		Limit:     1000,
		Step:      30000, //30sec
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)

	// testing retry
	retries, h := counter()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	// 3 retries configured.
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)
	rt.Close()

	rt, err = newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	// testing split interval
	count, h := promqlResult(matrix)
	rt.setHandler(h)
	resp, err := tpw(rt).RoundTrip(req)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)
	lokiResponse, err := lokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)

	// testing cache
	count, h = counter()
	rt.setHandler(h)
	cacheResp, err := tpw(rt).RoundTrip(req)
	// 0 queries, results are cached.
	require.Equal(t, 0, *count)
	require.NoError(t, err)
	lokiCacheResponse, err := lokiCodec.DecodeResponse(ctx, cacheResp, lreq)
	require.NoError(t, err)

	require.Equal(t, lokiResponse.(*LokiPromResponse).Response, lokiCacheResponse.(*LokiPromResponse).Response)
}

func TestLogFilterTripperware(t *testing.T) {

	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"} |= "foo"`,
		Limit:     1000,
		StartTs:   testTime.Add(-10 * time.Hour), // bigger than the limit
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	//testing limit
	count, h := promqlResult(streams)
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, 0, *count)
	require.Error(t, err)

	// set the query length back to normal
	lreq.StartTs = testTime.Add(-6 * time.Hour)
	req, err = lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	// testing retry
	retries, h := counter()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)
}

func TestSeriesTripperware(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiSeriesRequest{
		Match:   []string{`{job="varlogs"}`},
		StartTs: testTime.Add(-6 * time.Hour),
		EndTs:   testTime,
		Path:    "/loki/api/v1/series",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	count, h := seriesResult(series)
	rt.setHandler(h)

	resp, err := tpw(rt).RoundTrip(req)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)
	lokiSeriesResponse, err := lokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)
	res, ok := lokiSeriesResponse.(*LokiSeriesResponse)
	require.Equal(t, true, ok)
	expectedResponse := make([]logproto.SeriesIdentifier, 0, 3)
	expectedResponse = append(expectedResponse, series[0].seriesIndentifier...)
	expectedResponse = append(expectedResponse, series[1].seriesIndentifier...)
	require.Equal(t, expectedResponse, res.Data)

	// testing cache
	tests := []struct {
		name             string
		request          LokiSeriesRequest
		expectedResponse []logproto.SeriesIdentifier
		expectedCount    int
	}{
		{
			"no requests, results are cached",
			LokiSeriesRequest{
				Match:   []string{`{job="varlogs"}`},
				StartTs: testTime.Add(-6 * time.Hour),
				EndTs:   testTime,
				Path:    "/loki/api/v1/series",
			},
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[0],
				seriesIdentifierData[1],
				seriesIdentifierData[2],
			},
			0,
		},
		{
			"2 requests, one of the results are cached",
			LokiSeriesRequest{
				Match:   []string{`{job="varlogs"}`},
				StartTs: testTime.Add(-6 * time.Hour),
				EndTs:   testTime.Add(6 * time.Hour),
				Path:    "/loki/api/v1/series",
			},
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[0],
				seriesIdentifierData[1],
				seriesIdentifierData[2],
				seriesIdentifierData[3],
			},
			2,
		},
		{
			"1 request, no results are cached",
			LokiSeriesRequest{
				Match:   []string{`{job="varlogs"}`},
				StartTs: testTime.Add(-1 * time.Hour),
				EndTs:   testTime,
				Path:    "/loki/api/v1/series",
			},
			[]logproto.SeriesIdentifier{
				seriesIdentifierData[2],
			},
			1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "1")
			req, err := lokiCodec.EncodeRequest(ctx, &tc.request)
			require.NoError(t, err)

			count, h = seriesResult(series)
			rt.setHandler(h)

			cacheResp, err := tpw(rt).RoundTrip(req)
			require.Equal(t, tc.expectedCount, *count)
			require.NoError(t, err)

			lokiCacheResponse, err := lokiCodec.DecodeResponse(ctx, cacheResp, lreq)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResponse, lokiCacheResponse.(*LokiSeriesResponse).Data)
		})
	}
}
func TestLogNoRegex(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`, // no regex so it should go to the querier
		Limit:     1000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	count, h := promqlResult(streams)
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, 1, *count)
	require.NoError(t, err)
}

func TestUnhandledPath(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := http.NewRequest(http.MethodGet, "/loki/api/v1/labels", nil)
	require.NoError(t, err)
	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	count, h := errorResult()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, 1, *count)
	require.NoError(t, err)
}

func TestRegexpParamsSupport(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`, // no regex so it should go to the querier
		Limit:     1000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	// fudge a regexp params
	params := req.URL.Query()
	params.Set("regexp", "foo")
	req.URL.RawQuery = params.Encode()

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	count, h := promqlResult(streams)
	rt.setHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// the query params should contain the filter.
		require.Contains(t, r.URL.Query().Get("query"), `|~"foo"`)
		h.ServeHTTP(rw, r)
	}))
	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, 2, *count) // expecting the query to also be splitted since it has a filter.
	require.NoError(t, err)
}

func TestPostQueries(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/loki/api/v1/query_range", nil)
	data := url.Values{
		"query": {`{app="foo"} |~ "foo"`},
	}
	body := bytes.NewBufferString(data.Encode())
	req.Body = ioutil.NopCloser(body)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	req = req.WithContext(user.InjectOrgID(context.Background(), "1"))
	require.NoError(t, err)
	_, err = newRoundTripper(
		frontend.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected default roundtripper called")
			return nil, nil
		}),
		frontend.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		}),
		frontend.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected metric roundtripper called")
			return nil, nil
		}),
		frontend.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected series roundtripper called")
			return nil, nil
		}),
		fakeLimits{},
	).RoundTrip(req)
	require.NoError(t, err)
}

func TestEntriesLimitsTripperware(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{maxEntriesLimitPerQuery: 5000}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`, // no regex so it should go to the querier
		Limit:     10000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max entries limit per query exceeded, limit > max_entries_limit (10000 > 5000)"), err)
}

func TestEntriesLimitWithZeroTripperware(t *testing.T) {
	tpw, stoppers, err := NewTripperware(testConfig, util.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	defer stopCache(stoppers)
	require.NoError(t, err)

	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`, // no regex so it should go to the querier
		Limit:     10000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := lokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	_, err = tpw(rt).RoundTrip(req)
	require.NoError(t, err)
}

func stopCache(stoppers []Stopper) {
	for _, stopper := range stoppers {
		if stopper != nil {
			go func() {
				stopper.Stop()
			}()
		}
	}
}

type fakeLimits struct {
	maxQueryParallelism     int
	maxEntriesLimitPerQuery int
	splits                  map[string]time.Duration
}

func (f fakeLimits) QuerySplitDuration(key string) time.Duration {
	if f.splits == nil {
		return 0
	}
	return f.splits[key]
}

func (fakeLimits) MaxQueryLength(string) time.Duration {
	return time.Hour * 7
}

func (f fakeLimits) MaxQueryParallelism(string) int {
	if f.maxQueryParallelism == 0 {
		return 1
	}
	return f.maxQueryParallelism
}

func (f fakeLimits) MaxEntriesLimitPerQuery(string) int {
	return f.maxEntriesLimitPerQuery
}

func (f fakeLimits) MaxCacheFreshness(string) time.Duration {
	return 1 * time.Minute
}

func counter() (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		count++
	})
}

func errorResult() (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		count++
		w.WriteHeader(http.StatusInternalServerError)
	})
}

func promqlResult(v parser.Value) (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if err := marshal.WriteQueryResponseJSON(logql.Result{Data: v}, w); err != nil {
			panic(err)
		}
		count++
	})
}

type mockSeries struct {
	start             time.Time
	end               time.Time
	seriesIndentifier []logproto.SeriesIdentifier
}

func seriesResult(v []mockSeries) (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			panic(err)
		}
		req, err := loghttp.ParseSeriesQuery(r)
		if err != nil {
			panic(err)
		}
		var seriesResponse logproto.SeriesResponse
		for _, series := range v {
			if req.Start.UnixNano() >= series.end.UnixNano() || req.End.UnixNano() <= series.start.UnixNano() {
				continue
			}
			seriesResponse.Series = append(seriesResponse.Series, series.seriesIndentifier...)
		}

		lock.Lock()
		defer lock.Unlock()
		if err := marshal.WriteSeriesResponseJSON(seriesResponse, w); err != nil {
			panic(err)
		}
		count++
	})
}

type fakeRoundTripper struct {
	*httptest.Server
	host string
}

func newfakeRoundTripper() (*fakeRoundTripper, error) {
	s := httptest.NewServer(nil)
	u, err := url.Parse(s.URL)
	if err != nil {
		return nil, err
	}
	return &fakeRoundTripper{
		Server: s,
		host:   u.Host,
	}, nil
}

func (s *fakeRoundTripper) setHandler(h http.Handler) {
	s.Config.Handler = middleware.AuthenticateUser.Wrap(h)
}

func (s fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = "http"
	r.URL.Host = s.host
	return http.DefaultTransport.RoundTrip(r)
}

func toMs(t time.Time) int64 {
	return t.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}
