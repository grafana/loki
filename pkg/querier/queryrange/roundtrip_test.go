package queryrange

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
)

var (
	testTime   = time.Date(2019, 12, 02, 11, 10, 10, 10, time.UTC)
	testConfig = Config{
		IntervalBatchSize: 32,
		Config: queryrange.Config{
			SplitQueriesByInterval: 4 * time.Hour,
			AlignQueriesWithStep:   true,
			MaxRetries:             3,
			CacheResults:           true,
			ResultsCacheConfig: queryrange.ResultsCacheConfig{
				MaxCacheFreshness: 1 * time.Minute,
				SplitInterval:     4 * time.Hour,
				CacheConfig: cache.Config{
					EnableFifoCache: true,
					Fifocache: cache.FifoCacheConfig{
						Size:     1024,
						Validity: 24 * time.Hour,
					},
				},
			},
		},
	}
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
)

// those tests are mostly for testing the glue between all component and make sure they activate correctly.
func TestMetricsTripperware(t *testing.T) {

	tpw, stopper, err := NewTripperware(testConfig, util.Logger, fakeLimits{})
	if stopper != nil {
		defer func() { _ = stopper.Stop() }()
	}
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
	defer rt.Close()

	// testing retry
	retries, h := counter()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	// 3 retries configured.
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)

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
	// 0 queries result are cached.
	require.Equal(t, 0, *count)
	require.NoError(t, err)
	lokiCacheResponse, err := lokiCodec.DecodeResponse(ctx, cacheResp, lreq)
	require.NoError(t, err)

	require.Equal(t, lokiResponse, lokiCacheResponse)
}

func TestLogFilterTripperware(t *testing.T) {

	tpw, stopper, err := NewTripperware(testConfig, util.Logger, fakeLimits{})
	if stopper != nil {
		defer func() { _ = stopper.Stop() }()
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

	// testing split 2 queries with 32 batch size
	split, h := promqlResult(streams)
	rt.setHandler(h)
	resp, err := tpw(rt).RoundTrip(req)
	require.Equal(t, 64, *split)
	require.NoError(t, err)
	_, err = lokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)

	// testing retry
	retries, h := counter()
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)
}

func TestLogNoRegex(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, util.Logger, fakeLimits{})
	if stopper != nil {
		defer func() { _ = stopper.Stop() }()
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
	tpw, stopper, err := NewTripperware(testConfig, util.Logger, fakeLimits{})
	if stopper != nil {
		defer func() { _ = stopper.Stop() }()
	}
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

type fakeLimits struct{}

func (fakeLimits) MaxQueryLength(string) time.Duration {
	return time.Hour * 7
}

func (fakeLimits) MaxQueryParallelism(string) int {
	return 1
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

func promqlResult(v promql.Value) (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if err := marshal.WriteQueryResponseJSON(v, w); err != nil {
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
