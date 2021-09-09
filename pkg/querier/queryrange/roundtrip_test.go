package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
)

var (
	testTime   = time.Date(2019, 12, 02, 11, 10, 10, 10, time.UTC)
	testConfig = Config{queryrange.Config{
		SplitQueriesByInterval: 4 * time.Hour,
		AlignQueriesWithStep:   true,
		MaxRetries:             3,
		CacheResults:           true,
		ResultsCacheConfig: queryrange.ResultsCacheConfig{
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
	vector = promql.Vector{
		{
			Point: promql.Point{
				T: toMs(testTime.Add(-4 * time.Hour)),
				V: 0.013333333333333334,
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
	streams = logqlmodel.Streams{
		{
			Entries: []logproto.Entry{
				{Timestamp: testTime.Add(-4 * time.Hour), Line: "foo"},
				{Timestamp: testTime.Add(-1 * time.Hour), Line: "barr"},
			},
			Labels: `{filename="/var/hostlog/apport.log", job="varlogs"}`,
		},
	}

	series = logproto.SeriesResponse{
		Series: []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
			},
			{
				Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
			},
		},
	}
)

// those tests are mostly for testing the glue between all component and make sure they activate correctly.
func TestMetricsTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{maxSeries: math.MaxInt32}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &LokiRequest{
		Query:     `rate({app="foo"} |= "foo"[1m])`,
		Limit:     1000,
		Step:      30000, // 30sec
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/query_range",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
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
	lokiResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)

	// testing cache
	count, h = counter()
	rt.setHandler(h)
	cacheResp, err := tpw(rt).RoundTrip(req)
	// 0 queries result are cached.
	require.Equal(t, 0, *count)
	require.NoError(t, err)
	lokiCacheResponse, err := LokiCodec.DecodeResponse(ctx, cacheResp, lreq)
	require.NoError(t, err)

	require.Equal(t, lokiResponse.(*LokiPromResponse).Response, lokiCacheResponse.(*LokiPromResponse).Response)
}

func TestLogFilterTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
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
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	// testing limit
	count, h := promqlResult(streams)
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, 0, *count)
	require.Error(t, err)

	// set the query length back to normal
	lreq.StartTs = testTime.Add(-6 * time.Hour)
	req, err = LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	// testing retry
	retries, h := counter()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)
}

func TestInstantQueryTripperware(t *testing.T) {
	testShardingConfig := testConfig
	testShardingConfig.ShardedQueries = true
	tpw, stopper, err := NewTripperware(testShardingConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 1*time.Second, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiInstantRequest{
		Query:     `sum by (job) (bytes_rate({cluster="dev-us-central-0"}[15m]))`,
		Limit:     1000,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	count, h := promqlResult(vector)
	rt.setHandler(h)
	resp, err := tpw(rt).RoundTrip(req)
	require.Equal(t, 1, *count)
	require.NoError(t, err)

	lokiResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)
	require.IsType(t, &LokiPromResponse{}, lokiResponse)
}

func TestSeriesTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiSeriesRequest{
		Match:   []string{`{job="varlogs"}`},
		StartTs: testTime.Add(-25 * time.Hour), // bigger than the limit
		EndTs:   testTime,
		Path:    "/loki/api/v1/series",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
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
	lokiSeriesResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	res, ok := lokiSeriesResponse.(*LokiSeriesResponse)
	require.Equal(t, true, ok)

	// make sure we return unique series since responses from
	// SplitByInterval middleware might have duplicate series
	require.Equal(t, series.Series, res.Data)
	require.NoError(t, err)
}

func TestLabelsTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiLabelNamesRequest{
		StartTs: testTime.Add(-25 * time.Hour), // bigger than the limit
		EndTs:   testTime,
		Path:    "/loki/api/v1/labels",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	handler := newFakeHandler(
		// we expect 2 calls.
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, marshal.WriteLabelResponseJSON(logproto.LabelResponse{Values: []string{"foo", "bar", "blop"}}, w))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, marshal.WriteLabelResponseJSON(logproto.LabelResponse{Values: []string{"foo", "bar", "blip"}}, w))
		}),
	)
	rt.setHandler(handler)
	resp, err := tpw(rt).RoundTrip(req)
	// verify 2 calls have been made to downstream.
	require.Equal(t, 2, handler.count)
	require.NoError(t, err)
	lokiLabelsResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	res, ok := lokiLabelsResponse.(*LokiLabelNamesResponse)
	require.Equal(t, true, ok)
	require.Equal(t, []string{"foo", "bar", "blop", "blip"}, res.Data)
	require.Equal(t, "success", res.Status)
	require.NoError(t, err)
}

func TestLogNoRegex(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
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
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
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
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := http.NewRequest(http.MethodGet, "/loki/api/v1/labels/foo/values", nil)
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
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
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
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
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
		require.Contains(t, r.URL.Query().Get("query"), `|~ "foo"`)
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
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected default roundtripper called")
			return nil, nil
		}),
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		}),
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected metric roundtripper called")
			return nil, nil
		}),
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected series roundtripper called")
			return nil, nil
		}),
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected labels roundtripper called")
			return nil, nil
		}),
		queryrange.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected instant roundtripper called")
			return nil, nil
		}),
		fakeLimits{},
	).RoundTrip(req)
	require.NoError(t, err)
}

func TestEntriesLimitsTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{maxEntriesLimitPerQuery: 5000}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
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
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	_, err = tpw(rt).RoundTrip(req)
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max entries limit per query exceeded, limit > max_entries_limit (10000 > 5000)"), err)
}

func TestEntriesLimitWithZeroTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
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
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	_, err = tpw(rt).RoundTrip(req)
	require.NoError(t, err)
}

type fakeLimits struct {
	maxQueryParallelism     int
	maxEntriesLimitPerQuery int
	maxSeries               int
	splits                  map[string]time.Duration
	minShardingLookback     time.Duration
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

func (f fakeLimits) MaxQuerySeries(string) int {
	return f.maxSeries
}

func (f fakeLimits) MaxCacheFreshness(string) time.Duration {
	return 1 * time.Minute
}

func (f fakeLimits) MaxQueryLookback(string) time.Duration {
	return 0
}

func (f fakeLimits) MinShardingLookback(string) time.Duration {
	return f.minShardingLookback
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
		if err := marshal.WriteQueryResponseJSON(logqlmodel.Result{Data: v}, w); err != nil {
			panic(err)
		}
		count++
	})
}

func seriesResult(v logproto.SeriesResponse) (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if err := marshal.WriteSeriesResponseJSON(v, w); err != nil {
			panic(err)
		}
		count++
	})
}

type fakeHandler struct {
	count int
	lock  sync.Mutex
	calls []http.Handler
}

func newFakeHandler(calls ...http.Handler) *fakeHandler {
	return &fakeHandler{calls: calls}
}

func (f *fakeHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.calls[f.count].ServeHTTP(w, req)
	f.count++
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
