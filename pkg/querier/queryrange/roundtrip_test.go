package queryrange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
	valid "github.com/grafana/loki/v3/pkg/validation"
)

var (
	testTime   = time.Date(2019, 12, 2, 11, 10, 10, 10, time.UTC)
	testConfig = Config{
		Config: base.Config{
			AlignQueriesWithStep: true,
			MaxRetries:           3,
			CacheResults:         true,
			ResultsCacheConfig: base.ResultsCacheConfig{
				Config: resultscache.Config{
					CacheConfig: cache.Config{
						EmbeddedCache: cache.EmbeddedCacheConfig{
							Enabled:   true,
							MaxSizeMB: 1024,
							TTL:       24 * time.Hour,
						},
					},
				},
			},
		},
		Transformer:            nil,
		CacheIndexStatsResults: true,
		StatsCacheConfig: IndexStatsCacheConfig{
			ResultsCacheConfig: base.ResultsCacheConfig{
				Config: resultscache.Config{
					CacheConfig: cache.Config{
						EmbeddedCache: cache.EmbeddedCacheConfig{
							Enabled:   true,
							MaxSizeMB: 1024,
							TTL:       24 * time.Hour,
						},
					},
				},
			},
		},
		VolumeCacheConfig: VolumeCacheConfig{
			ResultsCacheConfig: base.ResultsCacheConfig{
				Config: resultscache.Config{
					CacheConfig: cache.Config{
						EmbeddedCache: cache.EmbeddedCacheConfig{
							Enabled:   true,
							MaxSizeMB: 1024,
							TTL:       24 * time.Hour,
						},
					},
				},
			},
		},
		CacheInstantMetricResults:    true,
		InstantMetricQuerySplitAlign: true,
		InstantMetricCacheConfig: InstantMetricCacheConfig{
			ResultsCacheConfig: base.ResultsCacheConfig{
				Config: resultscache.Config{
					CacheConfig: cache.Config{
						EmbeddedCache: cache.EmbeddedCacheConfig{
							Enabled:   true,
							MaxSizeMB: 1024,
							TTL:       24 * time.Hour,
						},
					},
				},
			},
		},
	}
	testEngineOpts = logql.EngineOpts{
		MaxLookBackPeriod: 30 * time.Second,
		LogExecutingQuery: false,
	}
	matrix = promql.Matrix{
		{
			Floats: []promql.FPoint{
				{
					T: toMs(testTime.Add(-4 * time.Hour)),
					F: 0.013333333333333334,
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
			T: toMs(testTime.Add(-4 * time.Hour)),
			F: 0.013333333333333334,
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
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "filename", Value: "/var/hostlog/apport.log"},
					{Key: "job", Value: "varlogs"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "filename", Value: "/var/hostlog/test.log"},
					{Key: "job", Value: "varlogs"},
				},
			},
		},
	}

	seriesVolume = logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 1024},
			{Name: `{bar="baz"}`, Volume: 3350},
		},
		Limit: 5,
	}
)

func getQueryAndStatsHandler(queryHandler, statsHandler base.Handler) base.Handler {
	return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
		switch r.(type) {
		case *logproto.IndexStatsRequest:
			return statsHandler.Do(ctx, r)
		case *LokiRequest, *LokiInstantRequest:
			return queryHandler.Do(ctx, r)
		}

		return nil, fmt.Errorf("Request not supported: %T", r)
	})
}

// those tests are mostly for testing the glue between all component and make sure they activate correctly.
func TestMetricsTripperware(t *testing.T) {
	var l Limits = fakeLimits{
		maxSeries:               math.MaxInt32,
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		maxQueryBytesRead:       1000,
		maxQuerierBytesRead:     100,
	}
	l = WithSplitByLimits(l, 4*time.Hour)
	noCacheTestCfg := testConfig
	noCacheTestCfg.CacheResults = false
	noCacheTestCfg.CacheIndexStatsResults = false
	tpw, stopper, err := NewMiddleware(noCacheTestCfg, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{
		Configs: testSchemasTSDB,
	}, nil, false, nil, constants.Loki)
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
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`rate({app="foo"} |= "foo"[1m])`),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	h := getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.ErrorContains(t, err, "query too large to execute on a single querier")
	require.Equal(t, 0, *queryCount)
	require.Equal(t, 2, *statsCount)

	// testing retry
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	retries, queryHandler := counterWithError(errors.New("handle error"))
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	// 3 retries configured.
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)

	// Configure with cache
	tpw, stopper, err = NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{
		Configs: testSchemasTSDB,
	}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	// testing split interval
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	count, queryHandler := promqlResult(matrix)
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	lokiResponse, err := tpw.Wrap(h).Do(ctx, lreq)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)

	// testing cache
	count, queryHandler = counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	lokiCacheResponse, err := tpw.Wrap(h).Do(ctx, lreq)
	// 0 queries result are cached.
	require.Equal(t, 0, *count)
	require.NoError(t, err)

	require.Equal(t, lokiResponse.(*LokiPromResponse).Response, lokiCacheResponse.(*LokiPromResponse).Response)
}

func TestLogFilterTripperware(t *testing.T) {
	var l Limits = fakeLimits{
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		maxQueryBytesRead:       1000,
		maxQuerierBytesRead:     100,
	}
	noCacheTestCfg := testConfig
	noCacheTestCfg.CacheResults = false
	noCacheTestCfg.CacheIndexStatsResults = false
	tpw, stopper, err := NewMiddleware(noCacheTestCfg, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &LokiRequest{
		Query:     `{app="foo"} |= "foo"`,
		Limit:     1000,
		StartTs:   testTime.Add(-10 * time.Hour), // bigger than the limit
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{app="foo"} |= "foo"`),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	// testing limit
	count, h := promqlResult(streams)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, 0, *count)
	require.Error(t, err)

	// set the query length back to normal
	lreq.StartTs = testTime.Add(-6 * time.Hour)

	// testing retry
	_, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	retries, queryHandler := counterWithError(errors.New("handler failed"))
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 2, *statsCount)
	require.Equal(t, 0, *queryCount)
}

func TestInstantQueryTripperwareResultCaching(t *testing.T) {
	// Goal is to make sure the instant query tripperware returns same results with and without cache middleware.
	// 1. Get result without cache middleware.
	// 2. Get result with middelware (with splitting). Result should be same.
	// 3. Make same query with middleware (this time hitting the cache). Result should be same.

	testLocal := testConfig
	testLocal.ShardedQueries = true
	testLocal.CacheResults = false
	testLocal.CacheIndexStatsResults = false
	testLocal.CacheInstantMetricResults = false
	l := fakeLimits{
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		maxQueryBytesRead:       1000,
		maxQuerierBytesRead:     100,
		queryTimeout:            1 * time.Minute,
		maxSeries:               1,
	}
	tpw, stopper, err := NewMiddleware(testLocal, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	q := `sum by (job) (bytes_rate({cluster="dev-us-central-0"}[15m]))`
	lreq := &LokiInstantRequest{
		Query:     q,
		Limit:     1000,
		TimeTs:    testTime.Add(-4 * time.Hour),
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(q),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	h := getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 2, *statsCount)
	require.Equal(t, 0, *queryCount)

	// 1. Without cache middleware.
	count, queryHandler := promqlResult(vector)
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	h = tpw.Wrap(h)
	lokiResponse, err := h.Do(ctx, lreq)
	require.Equal(t, 1, *count)
	require.NoError(t, err)

	exp, err := ResultToResponse(logqlmodel.Result{
		Data: vector,
	}, nil)
	require.NoError(t, err)
	expected := exp.(*LokiPromResponse)

	require.IsType(t, &LokiPromResponse{}, lokiResponse)
	concrete := lokiResponse.(*LokiPromResponse)
	require.Equal(t, loghttp.ResultTypeVector, concrete.Response.Data.ResultType)
	assertInstantSampleValues(t, expected, concrete) // assert actual sample values

	// 2. First time with caching enabled (no cache hit).
	testLocal.CacheInstantMetricResults = true
	l.instantMetricSplitDuration = map[string]time.Duration{
		// so making request [15] range, will have 2 subqueries aligned with [5m] giving total of [10m]. And 2 more subqueries for remaining [5m] aligning depending on exec time of the query.
		"1": 5 * time.Minute,
	}
	tpw, stopper, err = NewMiddleware(testLocal, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	count, queryHandler = promqlResult(vector)
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	lokiResponse, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, 4, *count) // split into 4 subqueries. like explained in `instantMetricSplitDuration` limits config.
	require.NoError(t, err)

	exp, err = ResultToResponse(logqlmodel.Result{
		Data: instantQueryResultWithCache(*count, 15*time.Minute, vector[0]),
	}, nil)
	require.NoError(t, err)
	expected = exp.(*LokiPromResponse)

	require.IsType(t, &LokiPromResponse{}, lokiResponse)
	concrete = lokiResponse.(*LokiPromResponse)
	require.Equal(t, loghttp.ResultTypeVector, concrete.Response.Data.ResultType)
	assertInstantSampleValues(t, expected, concrete) // assert actual sample values

	// 3. Second time with caching enabled (cache hit).
	count, queryHandler = promqlResult(vector)
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	lokiResponse, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, 0, *count) // no queries hit base handler, because all queries hit from cache.
	require.NoError(t, err)

	require.IsType(t, &LokiPromResponse{}, lokiResponse)
	concrete = lokiResponse.(*LokiPromResponse)
	require.Equal(t, loghttp.ResultTypeVector, concrete.Response.Data.ResultType)
	assertInstantSampleValues(t, expected, concrete) // assert actual sample values
}

func TestInstantQueryTripperware(t *testing.T) {
	testShardingConfigNoCache := testConfig
	testShardingConfigNoCache.ShardedQueries = true
	testShardingConfigNoCache.CacheResults = false
	testShardingConfigNoCache.CacheIndexStatsResults = false
	var l Limits = fakeLimits{
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		maxQueryBytesRead:       1000,
		maxQuerierBytesRead:     100,
		queryTimeout:            1 * time.Minute,
		maxSeries:               1,
	}
	tpw, stopper, err := NewMiddleware(testShardingConfigNoCache, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	q := `sum by (job) (bytes_rate({cluster="dev-us-central-0"}[15m]))`
	lreq := &LokiInstantRequest{
		Query:     q,
		Limit:     1000,
		TimeTs:    testTime.Add(-4 * time.Hour), // because vector data we return from mock handler has that time.
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(q),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	h := getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Error(t, err)
	require.Equal(t, 2, *statsCount)
	require.Equal(t, 0, *queryCount)

	count, queryHandler := promqlResult(vector)
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	h = getQueryAndStatsHandler(queryHandler, statsHandler)
	lokiResponse, err := tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, 1, *count)
	require.NoError(t, err)

	require.IsType(t, &LokiPromResponse{}, lokiResponse)
	concrete := lokiResponse.(*LokiPromResponse)
	require.Equal(t, loghttp.ResultTypeVector, concrete.Response.Data.ResultType)
}

func TestSeriesTripperware(t *testing.T) {
	l := fakeLimits{
		maxQueryLength:      48 * time.Hour,
		maxQueryParallelism: 1,
		metadataSplitDuration: map[string]time.Duration{
			"1": 24 * time.Hour,
		},
	}
	tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &LokiSeriesRequest{
		Match:   []string{`{job="varlogs"}`},
		StartTs: testTime.Add(-25 * time.Hour), // bigger than split by interval limit
		EndTs:   testTime,
		Path:    "/loki/api/v1/series",
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	count, h := seriesResult(series)
	lokiSeriesResponse, err := tpw.Wrap(h).Do(ctx, lreq)
	require.NoError(t, err)

	// 2 queries
	require.Equal(t, 2, *count)
	res, ok := lokiSeriesResponse.(*LokiSeriesResponse)
	require.True(t, ok)

	// make sure we return unique series since responses from
	// SplitByInterval middleware might have duplicate series
	require.Equal(t, series.Series, res.Data)
	require.NoError(t, err)
}

func TestLabelsTripperware(t *testing.T) {
	l := fakeLimits{
		maxQueryLength:      48 * time.Hour,
		maxQueryParallelism: 1,
		metadataSplitDuration: map[string]time.Duration{
			"1": 24 * time.Hour,
		},
	}
	tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := NewLabelRequest(
		testTime.Add(-25*time.Hour), // bigger than the limit
		testTime,
		"",
		"",
		"/loki/api/v1/labels",
	)

	ctx := user.InjectOrgID(context.Background(), "1")

	handler := newFakeHandler(
		// we expect 2 calls.
		base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
			return &LokiLabelNamesResponse{
				Status:  "success",
				Data:    []string{"foo", "bar", "blop"},
				Version: uint32(1),
			}, nil
		}),
		base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
			return &LokiLabelNamesResponse{
				Status:  "success",
				Data:    []string{"foo", "bar", "blip"},
				Version: uint32(1),
			}, nil
		}),
	)
	lokiLabelsResponse, err := tpw.Wrap(handler).Do(ctx, lreq)
	require.NoError(t, err)

	// verify 2 calls have been made to downstream.
	require.Equal(t, 2, handler.count)
	res, ok := lokiLabelsResponse.(*LokiLabelNamesResponse)
	require.True(t, ok)
	require.Equal(t, []string{"foo", "bar", "blop", "blip"}, res.Data)
	require.Equal(t, "success", res.Status)
	require.NoError(t, err)
}

func TestIndexStatsTripperware(t *testing.T) {
	tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &logproto.IndexStatsRequest{
		Matchers: `{job="varlogs"}`,
		From:     model.TimeFromUnixNano(testTime.Add(-25 * time.Hour).UnixNano()), // bigger than split by interval limit
		Through:  model.TimeFromUnixNano(testTime.UnixNano()),
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	response := logproto.IndexStatsResponse{
		Streams: 100,
		Chunks:  200,
		Bytes:   300,
		Entries: 400,
	}

	count, h := indexStatsResult(response)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)

	// Test the cache.
	// It should have the answer already so the query handler shouldn't be hit
	count, h = indexStatsResult(response)
	indexStatsResponse, err := tpw.Wrap(h).Do(ctx, lreq)
	require.NoError(t, err)
	require.Equal(t, 0, *count)

	// Test the response is the expected
	res, ok := indexStatsResponse.(*IndexStatsResponse)
	require.Equal(t, true, ok)
	require.Equal(t, response.Streams*2, res.Response.Streams)
	require.Equal(t, response.Chunks*2, res.Response.Chunks)
	require.Equal(t, response.Bytes*2, res.Response.Bytes)
	require.Equal(t, response.Entries*2, res.Response.Entries)
}

func TestVolumeTripperware(t *testing.T) {
	t.Run("instant queries hardcode step to 0 and return a prometheus style vector response", func(t *testing.T) {
		limits := fakeLimits{
			maxQueryLength: 48 * time.Hour,
			volumeEnabled:  true,
			maxSeries:      42,
		}
		tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
		if stopper != nil {
			defer stopper.Stop()
		}
		require.NoError(t, err)

		lreq := &logproto.VolumeRequest{
			Matchers:    `{job="varlogs"}`,
			From:        model.TimeFromUnixNano(testTime.Add(-25 * time.Hour).UnixNano()), // bigger than split by interval limit
			Through:     model.TimeFromUnixNano(testTime.UnixNano()),
			Limit:       10,
			Step:        0, // Setting this value to 0 is what makes this an instant query
			AggregateBy: seriesvolume.DefaultAggregateBy,
		}

		ctx := user.InjectOrgID(context.Background(), "1")

		count, h := seriesVolumeResult(seriesVolume)

		volumeResp, err := tpw.Wrap(h).Do(ctx, lreq)
		require.NoError(t, err)
		require.Equal(t, 2, *count) // 2 queries from splitting

		expected := base.PrometheusData{
			ResultType: loghttp.ResultTypeVector,
			Result: []base.SampleStream{
				{
					Labels: []logproto.LabelAdapter{{
						Name:  "bar",
						Value: "baz",
					}},
					Samples: []logproto.LegacySample{{
						Value:       6700,
						TimestampMs: testTime.Unix() * 1e3,
					}},
				},
				{
					Labels: []logproto.LabelAdapter{{
						Name:  "foo",
						Value: "bar",
					}},
					Samples: []logproto.LegacySample{{
						Value:       2048,
						TimestampMs: testTime.Unix() * 1e3,
					}},
				},
			},
		}

		res, ok := volumeResp.(*LokiPromResponse)
		require.Equal(t, true, ok)
		require.Equal(t, "success", res.Response.Status)
		require.Equal(t, expected, res.Response.Data)
	})

	t.Run("range queries return a prometheus style metrics response, putting volumes in buckets based on the step", func(t *testing.T) {
		tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, volumeEnabled: true}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
		if stopper != nil {
			defer stopper.Stop()
		}
		require.NoError(t, err)

		start := testTime.Add(-5 * time.Hour)
		end := testTime

		lreq := &logproto.VolumeRequest{
			Matchers:    `{job="varlogs"}`,
			From:        model.TimeFromUnixNano(start.UnixNano()), // bigger than split by interval limit
			Through:     model.TimeFromUnixNano(end.UnixNano()),
			Step:        time.Hour.Milliseconds(), // a non-zero value makes this a range query
			Limit:       10,
			AggregateBy: seriesvolume.DefaultAggregateBy,
		}

		ctx := user.InjectOrgID(context.Background(), "1")

		count, h := seriesVolumeResult(seriesVolume)

		volumeResp, err := tpw.Wrap(h).Do(ctx, lreq)
		require.NoError(t, err)

		/*
		   testTime is 2019-12-02T6:10:10Z
		   so with a 1 hour split, we expect 6 queries to be made:
		   6:10 -> 7, 7 -> 8, 8 -> 9, 9 -> 10, 10 -> 11, 11 -> 11:10
		*/
		require.Equal(t, 6, *count) // 6 queries from splitting into step buckets

		barBazExpectedSamples := []logproto.LegacySample{}
		util.ForInterval(time.Hour, start, end, true, func(_, e time.Time) {
			barBazExpectedSamples = append(barBazExpectedSamples, logproto.LegacySample{
				Value:       3350,
				TimestampMs: e.UnixMilli(),
			})
		})
		sort.Slice(barBazExpectedSamples, func(i, j int) bool {
			return barBazExpectedSamples[i].TimestampMs < barBazExpectedSamples[j].TimestampMs
		})

		fooBarExpectedSamples := []logproto.LegacySample{}
		util.ForInterval(time.Hour, start, end, true, func(_, e time.Time) {
			fooBarExpectedSamples = append(fooBarExpectedSamples, logproto.LegacySample{
				Value:       1024,
				TimestampMs: e.UnixMilli(),
			})
		})
		sort.Slice(fooBarExpectedSamples, func(i, j int) bool {
			return fooBarExpectedSamples[i].TimestampMs < fooBarExpectedSamples[j].TimestampMs
		})

		expected := base.PrometheusData{
			ResultType: loghttp.ResultTypeMatrix,
			Result: []base.SampleStream{
				{
					Labels: []logproto.LabelAdapter{{
						Name:  "bar",
						Value: "baz",
					}},
					Samples: barBazExpectedSamples,
				},
				{
					Labels: []logproto.LabelAdapter{{
						Name:  "foo",
						Value: "bar",
					}},
					Samples: fooBarExpectedSamples,
				},
			},
		}

		res, ok := volumeResp.(*LokiPromResponse)
		require.Equal(t, true, ok)
		require.Equal(t, "success", res.Response.Status)
		require.Equal(t, expected, res.Response.Data)
	})
}

func TestNewTripperware_Caches(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    Config
		numCaches int
		err       string
	}{
		{
			name: "results cache disabled, stats cache disabled",
			config: Config{
				Config: base.Config{
					CacheResults: false,
				},
				CacheIndexStatsResults: false,
			},
			numCaches: 0,
			err:       "",
		},
		{
			name: "results cache enabled",
			config: Config{
				Config: base.Config{
					CacheResults: true,
					ResultsCacheConfig: base.ResultsCacheConfig{
						Config: resultscache.Config{
							CacheConfig: cache.Config{
								EmbeddedCache: cache.EmbeddedCacheConfig{
									MaxSizeMB: 1,
									Enabled:   true,
								},
							},
						},
					},
				},
			},
			numCaches: 1,
			err:       "",
		},
		{
			name: "stats cache enabled",
			config: Config{
				CacheIndexStatsResults: true,
				StatsCacheConfig: IndexStatsCacheConfig{
					ResultsCacheConfig: base.ResultsCacheConfig{
						Config: resultscache.Config{
							CacheConfig: cache.Config{
								EmbeddedCache: cache.EmbeddedCacheConfig{
									Enabled:   true,
									MaxSizeMB: 1000,
								},
							},
						},
					},
				},
			},
			numCaches: 1,
			err:       "",
		},
		{
			name: "results cache enabled, stats cache enabled",
			config: Config{
				Config: base.Config{
					CacheResults: true,
					ResultsCacheConfig: base.ResultsCacheConfig{
						Config: resultscache.Config{
							CacheConfig: cache.Config{
								EmbeddedCache: cache.EmbeddedCacheConfig{
									Enabled:   true,
									MaxSizeMB: 2000,
								},
							},
						},
					},
				},
				CacheIndexStatsResults: true,
				StatsCacheConfig: IndexStatsCacheConfig{
					ResultsCacheConfig: base.ResultsCacheConfig{
						Config: resultscache.Config{
							CacheConfig: cache.Config{
								EmbeddedCache: cache.EmbeddedCacheConfig{
									Enabled:   true,
									MaxSizeMB: 1000,
								},
							},
						},
					},
				},
			},
			numCaches: 2,
			err:       "",
		},
		{
			name: "results cache enabled (no config provided)",
			config: Config{
				Config: base.Config{
					CacheResults: true,
				},
			},
			err: fmt.Sprintf("%s cache is not configured", stats.ResultCache),
		},
		{
			name: "stats cache enabled (no config provided)",
			config: Config{
				CacheIndexStatsResults: true,
			},
			numCaches: 0,
			err:       fmt.Sprintf("%s cache is not configured", stats.StatsResultCache),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stopper, err := NewMiddleware(tc.config, testEngineOpts, nil, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
			if stopper != nil {
				defer stopper.Stop()
			}

			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}

			require.NoError(t, err)
			require.IsType(t, StopperWrapper{}, stopper)

			var caches []cache.Cache
			for _, s := range stopper.(StopperWrapper) {
				if s != nil {
					c, ok := s.(cache.Cache)
					require.True(t, ok)

					require.NotNil(t, c)
					caches = append(caches, c)
				}
			}

			require.Equal(t, tc.numCaches, len(caches))
		})
	}
}

func TestLogNoFilter(t *testing.T) {
	tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, fakeLimits{maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &LokiRequest{
		Query:     `{app="foo"}`,
		Limit:     1000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{app="foo"}`),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	count, h := promqlResult(streams)
	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, 1, *count)
	require.Nil(t, err)
}

func TestPostQueries(t *testing.T) {
	lreq := &LokiRequest{
		Query: `{app="foo"} |~ "foo"`,
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{app="foo"} |~ "foo"`),
		},
	}
	ctx := user.InjectOrgID(context.Background(), "1")
	handler := base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) {
		t.Error("unexpected default roundtripper called")
		return nil, nil
	})
	_, err := newRoundTripper(
		util_log.Logger,
		handler,
		handler,
		base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) {
			return nil, nil
		}),
		handler,
		handler,
		handler,
		handler,
		handler,
		handler,
		handler,
		handler,
		fakeLimits{},
	).Do(ctx, lreq)
	require.NoError(t, err)
}

func TestTripperware_EntriesLimit(t *testing.T) {
	tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, fakeLimits{maxEntriesLimitPerQuery: 5000, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	lreq := &LokiRequest{
		Query:     `{app="foo"}`,
		Limit:     10000,
		StartTs:   testTime.Add(-6 * time.Hour),
		EndTs:     testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{app="foo"}`),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "1")

	called := false
	h := base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) {
		called = true
		return nil, nil
	})

	_, err = tpw.Wrap(h).Do(ctx, lreq)
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max entries limit per query exceeded, limit > max_entries_limit_per_query (10000 > 5000)"), err)
	require.False(t, called)
}

func TestTripperware_RequiredLabels(t *testing.T) {
	const noErr = ""

	for _, test := range []struct {
		qs            string
		expectedError string
		response      parser.Value
	}{
		{`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, noErr, vector},
		{`count_over_time({app="foo"}[1m]) / count_over_time({app="bar"}[1m] offset 1m)`, noErr, vector},
		{`count_over_time({app="foo"}[1m]) / count_over_time({pod="bar"}[1m] offset 1m)`, "stream selector is missing required matchers [app], labels present in the query were [pod]", nil},
		{`avg(count_over_time({pod=~"foo|bar"} |~".+bar" [1m]))`, "stream selector is missing required matchers [app], labels present in the query were [pod]", nil},
		{`{app="foo", pod="bar"}`, noErr, streams},
		{`{pod="bar"} |= "foo" |~ ".+bar"`, "stream selector is missing required matchers [app], labels present in the query were [pod]", nil},
	} {
		t.Run(test.qs, func(t *testing.T) {
			limits := fakeLimits{maxEntriesLimitPerQuery: 5000, maxQueryParallelism: 1, requiredLabels: []string{"app"}}
			tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
			if stopper != nil {
				defer stopper.Stop()
			}
			require.NoError(t, err)
			_, h := promqlResult(test.response)

			lreq := &LokiRequest{
				Query:     test.qs,
				Limit:     1000,
				StartTs:   testTime.Add(-6 * time.Hour),
				EndTs:     testTime,
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query_range",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(test.qs),
				},
			}
			// See loghttp.step
			step := time.Duration(int(math.Max(math.Floor(lreq.EndTs.Sub(lreq.StartTs).Seconds()/250), 1))) * time.Second
			lreq.Step = step.Milliseconds()

			ctx := user.InjectOrgID(context.Background(), "1")

			_, err = tpw.Wrap(h).Do(ctx, lreq)
			if test.expectedError != "" {
				require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "%s", test.expectedError), err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTripperware_RequiredNumberLabels(t *testing.T) {
	const noErr = ""

	for _, tc := range []struct {
		desc                 string
		query                string
		requiredNumberLabels int
		response             parser.Value
		expectedError        string
	}{
		{
			desc:                 "Log query - Limit disabled",
			query:                `{foo="foo"}`,
			requiredNumberLabels: 0,
			expectedError:        noErr,
			response:             streams,
		},
		{
			desc:                 "Log query - Below limit",
			query:                `{foo="foo"}`,
			requiredNumberLabels: 2,
			expectedError:        fmt.Sprintf(requiredNumberLabelsErrTmpl, "foo", 1, 2),
			response:             nil,
		},
		{
			desc:                 "Log query - On limit",
			query:                `{foo="foo", bar="bar"}`,
			requiredNumberLabels: 2,
			expectedError:        noErr,
			response:             streams,
		},
		{
			desc:                 "Log query - Over limit",
			query:                `{foo="foo", bar="bar", baz="baz"}`,
			requiredNumberLabels: 2,
			expectedError:        noErr,
			response:             streams,
		},
		{
			desc:                 "Metric query - Limit disabled",
			query:                `count_over_time({foo="foo"} [1m])`,
			requiredNumberLabels: 0,
			expectedError:        noErr,
			response:             vector,
		},
		{
			desc:                 "Metric query - Below limit",
			query:                `count_over_time({foo="foo"} [1m])`,
			requiredNumberLabels: 2,
			expectedError:        fmt.Sprintf(requiredNumberLabelsErrTmpl, "foo", 1, 2),
			response:             nil,
		},
		{
			desc:                 "Metric query - On limit",
			query:                `count_over_time({foo="foo", bar="bar"} [1m])`,
			requiredNumberLabels: 2,
			expectedError:        noErr,
			response:             vector,
		},
		{
			desc:                 "Metric query - Over limit",
			query:                `count_over_time({foo="foo", bar="bar", baz="baz"} [1m])`,
			requiredNumberLabels: 2,
			expectedError:        noErr,
			response:             vector,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			limits := fakeLimits{
				maxQueryParallelism:  1,
				requiredNumberLabels: tc.requiredNumberLabels,
			}
			tpw, stopper, err := NewMiddleware(testConfig, testEngineOpts, nil, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemas}, nil, false, nil, constants.Loki)
			if stopper != nil {
				defer stopper.Stop()
			}
			require.NoError(t, err)

			_, h := promqlResult(tc.response)

			lreq := &LokiRequest{
				Query:     tc.query,
				Limit:     1000,
				StartTs:   testTime.Add(-6 * time.Hour),
				EndTs:     testTime,
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query_range",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(tc.query),
				},
			}
			// See loghttp.step
			step := time.Duration(int(math.Max(math.Floor(lreq.EndTs.Sub(lreq.StartTs).Seconds()/250), 1))) * time.Second
			lreq.Step = step.Milliseconds()

			ctx := user.InjectOrgID(context.Background(), "1")

			_, err = tpw.Wrap(h).Do(ctx, lreq)
			if tc.expectedError != noErr {
				require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "%s", tc.expectedError), err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_getOperation(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		expectedOp string
	}{
		{
			name:       "instant_query",
			path:       "/loki/api/v1/query",
			expectedOp: InstantQueryOp,
		},
		{
			name:       "range_query_prom",
			path:       "/api/prom/query",
			expectedOp: QueryRangeOp,
		},
		{
			name:       "range_query",
			path:       "/loki/api/v1/query_range",
			expectedOp: QueryRangeOp,
		},
		{
			name:       "series_query",
			path:       "/loki/api/v1/series",
			expectedOp: SeriesOp,
		},
		{
			name:       "series_query_prom",
			path:       "/api/prom/series",
			expectedOp: SeriesOp,
		},
		{
			name:       "labels_query",
			path:       "/loki/api/v1/labels",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "labels_query_prom",
			path:       "/api/prom/labels",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_query",
			path:       "/loki/api/v1/label",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "labels_query_prom",
			path:       "/api/prom/label",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_values_query",
			path:       "/loki/api/v1/label/__name__/values",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_values_query_prom",
			path:       "/api/prom/label/__name__/values",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "detected_fields",
			path:       "/loki/api/v1/detected_fields",
			expectedOp: DetectedFieldsOp,
		},
		{
			name:       "detected_fields_values",
			path:       "/loki/api/v1/detected_field/foo/values",
			expectedOp: DetectedFieldsOp,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getOperation(tc.path)
			assert.Equal(t, tc.expectedOp, got)
		})
	}
}

func TestMetricsTripperware_SplitShardStats(t *testing.T) {
	l := WithSplitByLimits(fakeLimits{
		maxSeries:               math.MaxInt32,
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		queryTimeout:            1 * time.Minute,
	}, 1*time.Hour) // 1 hour split time interval

	statsTestCfg := testConfig
	statsTestCfg.ShardedQueries = true
	statsSchemas := testSchemas
	statsSchemas[0].RowShards = 4

	for _, tc := range []struct {
		name               string
		request            base.Request
		expectedSplitStats int64
		expectedShardStats int64
	}{
		{
			name: "instant query split",
			request: &LokiInstantRequest{
				Query:     `sum by (app) (rate({app="foo"} |= "foo"[2h]))`,
				Limit:     1000,
				TimeTs:    testTime,
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (app) (rate({app="foo"} |= "foo"[2h]))`),
				},
			},
			// [2h] interval split by 1h configured split interval.
			// Also since we split align(testConfig.InstantQuerySplitAlign=true) with split interval (1h).
			// 1 subquery will be exactly [1h] and 2 more subqueries to align with `testTime` used in query's TimeTs.
			expectedSplitStats: 3,
			expectedShardStats: 12, // 3 time splits * 4 row shards
		},
		{
			name: "instant query split not split",
			request: &LokiInstantRequest{
				Query:     `sum by (app) (rate({app="foo"} |= "foo"[1h]))`,
				Limit:     1000,
				TimeTs:    testTime,
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (app) (rate({app="foo"} |= "foo"[1h]))`),
				},
			},
			expectedSplitStats: 0, // [1h] interval not split
			expectedShardStats: 4, // 4 row shards
		},
		{
			name: "range query split",
			request: &LokiRequest{
				Query:     `sum by (app) (rate({app="foo"} |= "foo"[1h]))`,
				Limit:     1000,
				Step:      30000, // 30sec
				StartTs:   testTime.Add(-2 * time.Hour),
				EndTs:     testTime,
				Direction: logproto.FORWARD,
				Path:      "/query_range",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (app) (rate({app="foo"} |= "foo"[1h]))`),
				},
			},
			expectedSplitStats: 3,  // 2 hour range interval split based on the base hour + the remainder
			expectedShardStats: 12, // 3 time splits * 4 row shards
		},
		{
			name: "range query not split",
			request: &LokiRequest{
				Query:     `sum by (app) (rate({app="foo"} |= "foo"[1h]))`,
				Limit:     1000,
				Step:      30000, // 30sec
				StartTs:   testTime.Add(-1 * time.Minute),
				EndTs:     testTime,
				Direction: logproto.FORWARD,
				Path:      "/query_range",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (app) (rate({app="foo"} |= "foo"[1h]))`),
				},
			},
			expectedSplitStats: 0, // 1 minute range interval not split
			expectedShardStats: 4, // 4 row shards
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tpw, stopper, err := NewMiddleware(statsTestCfg, testEngineOpts, nil, util_log.Logger, l, config.SchemaConfig{Configs: statsSchemas}, nil, false, nil, constants.Loki)
			if stopper != nil {
				defer stopper.Stop()
			}
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "1")

			_, h := promqlResult(matrix)
			lokiResponse, err := tpw.Wrap(h).Do(ctx, tc.request)
			require.NoError(t, err)

			require.Equal(t, tc.expectedSplitStats, lokiResponse.(*LokiPromResponse).Statistics.Summary.Splits)
			require.Equal(t, tc.expectedShardStats, lokiResponse.(*LokiPromResponse).Statistics.Summary.Shards)
		})
	}
}

type fakeLimits struct {
	maxQueryLength              time.Duration
	maxQueryParallelism         int
	tsdbMaxQueryParallelism     int
	maxQueryLookback            time.Duration
	maxEntriesLimitPerQuery     int
	maxSeries                   int
	splitDuration               map[string]time.Duration
	metadataSplitDuration       map[string]time.Duration
	recentMetadataSplitDuration map[string]time.Duration
	recentMetadataQueryWindow   map[string]time.Duration
	instantMetricSplitDuration  map[string]time.Duration
	ingesterSplitDuration       map[string]time.Duration
	minShardingLookback         time.Duration
	queryTimeout                time.Duration
	requiredLabels              []string
	requiredNumberLabels        int
	maxQueryBytesRead           int
	maxQuerierBytesRead         int
	maxStatsCacheFreshness      time.Duration
	maxMetadataCacheFreshness   time.Duration
	volumeEnabled               bool
}

func (f fakeLimits) QuerySplitDuration(key string) time.Duration {
	if f.splitDuration == nil {
		return 0
	}
	return f.splitDuration[key]
}

func (f fakeLimits) InstantMetricQuerySplitDuration(key string) time.Duration {
	if f.instantMetricSplitDuration == nil {
		return 0
	}
	return f.instantMetricSplitDuration[key]
}

func (f fakeLimits) MetadataQuerySplitDuration(key string) time.Duration {
	if f.metadataSplitDuration == nil {
		return 0
	}
	return f.metadataSplitDuration[key]
}

func (f fakeLimits) RecentMetadataQuerySplitDuration(key string) time.Duration {
	if f.recentMetadataSplitDuration == nil {
		return 0
	}
	return f.recentMetadataSplitDuration[key]
}

func (f fakeLimits) RecentMetadataQueryWindow(key string) time.Duration {
	if f.recentMetadataQueryWindow == nil {
		return 0
	}
	return f.recentMetadataQueryWindow[key]
}

func (f fakeLimits) IngesterQuerySplitDuration(key string) time.Duration {
	if f.ingesterSplitDuration == nil {
		return 0
	}
	return f.ingesterSplitDuration[key]
}

func (f fakeLimits) MaxQueryLength(context.Context, string) time.Duration {
	if f.maxQueryLength == 0 {
		return time.Hour * 7
	}
	return f.maxQueryLength
}

func (f fakeLimits) MaxQueryRange(context.Context, string) time.Duration {
	return time.Second
}

func (f fakeLimits) MaxQueryParallelism(context.Context, string) int {
	return f.maxQueryParallelism
}

func (f fakeLimits) TSDBMaxQueryParallelism(context.Context, string) int {
	return f.tsdbMaxQueryParallelism
}

func (f fakeLimits) MaxEntriesLimitPerQuery(context.Context, string) int {
	return f.maxEntriesLimitPerQuery
}

func (f fakeLimits) MaxQuerySeries(context.Context, string) int {
	return f.maxSeries
}

func (f fakeLimits) MaxCacheFreshness(context.Context, string) time.Duration {
	return 1 * time.Minute
}

func (f fakeLimits) MaxQueryLookback(context.Context, string) time.Duration {
	return f.maxQueryLookback
}

func (f fakeLimits) MinShardingLookback(string) time.Duration {
	return f.minShardingLookback
}

func (f fakeLimits) MaxQueryBytesRead(context.Context, string) int {
	return f.maxQueryBytesRead
}

func (f fakeLimits) MaxQuerierBytesRead(context.Context, string) int {
	return f.maxQuerierBytesRead
}

func (f fakeLimits) QueryTimeout(context.Context, string) time.Duration {
	return f.queryTimeout
}

func (f fakeLimits) BlockedQueries(context.Context, string) []*validation.BlockedQuery {
	return []*validation.BlockedQuery{}
}

func (f fakeLimits) RequiredLabels(context.Context, string) []string {
	return f.requiredLabels
}

func (f fakeLimits) RequiredNumberLabels(_ context.Context, _ string) int {
	return f.requiredNumberLabels
}

func (f fakeLimits) MaxStatsCacheFreshness(_ context.Context, _ string) time.Duration {
	return f.maxStatsCacheFreshness
}

func (f fakeLimits) MaxMetadataCacheFreshness(_ context.Context, _ string) time.Duration {
	return f.maxMetadataCacheFreshness
}

func (f fakeLimits) VolumeEnabled(_ string) bool {
	return f.volumeEnabled
}

func (f fakeLimits) TSDBMaxBytesPerShard(_ string) int {
	return valid.DefaultTSDBMaxBytesPerShard
}

func (f fakeLimits) TSDBShardingStrategy(string) string {
	return logql.PowerOfTwoVersion.String()
}

func (f fakeLimits) ShardAggregations(string) []string {
	return nil
}

type ingesterQueryOpts struct {
	queryStoreOnly       bool
	queryIngestersWithin time.Duration
}

func (i ingesterQueryOpts) QueryStoreOnly() bool {
	return i.queryStoreOnly
}

func (i ingesterQueryOpts) QueryIngestersWithin() time.Duration {
	return i.queryIngestersWithin
}

func counter() (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		return base.NewEmptyPrometheusResponse(model.ValMatrix), nil
	})
}

func counterWithError(err error) (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		return nil, err
	})
}

func promqlResult(v parser.Value) (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, r base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		params, err := ParamsFromRequest(r)
		if err != nil {
			return nil, err
		}
		result := logqlmodel.Result{Data: v}
		return ResultToResponse(result, params)
	})
}

func seriesResult(v logproto.SeriesResponse) (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		return &LokiSeriesResponse{
			Status:  "success",
			Version: 1,
			Data:    v.Series,
		}, nil
	})
}

func indexStatsResult(v logproto.IndexStatsResponse) (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		return &IndexStatsResponse{Response: &v}, nil
	})
}

func seriesVolumeResult(v logproto.VolumeResponse) (*int, base.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, base.HandlerFunc(func(_ context.Context, _ base.Request) (base.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		count++
		return &VolumeResponse{Response: &v}, nil
	})
}

// instantQueryResultWithCache used when instant query tripperware is created with split align and cache middleware.
// Assuming each subquery handler returns `val` sample, then this function returns overal result by combining all the subqueries sample values.
func instantQueryResultWithCache(split int, ts time.Duration, val promql.Sample) promql.Vector {
	v := (val.F * float64(split)) / ts.Seconds()
	return promql.Vector{
		promql.Sample{
			T:      val.T,
			F:      v,
			H:      val.H,
			Metric: val.Metric,
		},
	}
}

func assertInstantSampleValues(t *testing.T, exp *LokiPromResponse, got *LokiPromResponse) {
	expR := exp.Response.Data.Result
	gotR := got.Response.Data.Result

	expSamples := make([]logproto.LegacySample, 0)
	for _, v := range expR {
		expSamples = append(expSamples, v.Samples...)
	}

	gotSamples := make([]logproto.LegacySample, 0)
	for _, v := range gotR {
		gotSamples = append(gotSamples, v.Samples...)
	}

	require.Equal(t, expSamples, gotSamples)
}

type fakeHandler struct {
	count int
	lock  sync.Mutex
	calls []base.Handler
}

func newFakeHandler(calls ...base.Handler) *fakeHandler {
	return &fakeHandler{calls: calls}
}

func (f *fakeHandler) Do(ctx context.Context, req base.Request) (base.Response, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	r, err := f.calls[f.count].Do(ctx, req)
	f.count++
	return r, err
}

func toMs(t time.Time) int64 {
	return t.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}
