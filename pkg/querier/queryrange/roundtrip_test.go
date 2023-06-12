package queryrange

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	testTime   = time.Date(2019, 12, 2, 11, 10, 10, 10, time.UTC)
	testConfig = Config{
		Config: queryrangebase.Config{
			AlignQueriesWithStep: true,
			MaxRetries:           3,
			CacheResults:         true,
			ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
				CacheConfig: cache.Config{
					EnableFifoCache: true,
					Fifocache: cache.FifoCacheConfig{
						MaxSizeItems: 1024,
						TTL:          24 * time.Hour,
					},
				},
			},
		},
		Transformer:            nil,
		CacheIndexStatsResults: true,
		StatsCacheConfig: IndexStatsCacheConfig{
			ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
				CacheConfig: cache.Config{
					EnableFifoCache: true,
					Fifocache: cache.FifoCacheConfig{
						MaxSizeItems: 1024,
						TTL:          24 * time.Hour,
					},
				},
			},
		},
	}
	testEngineOpts = logql.EngineOpts{
		Timeout:           30 * time.Second,
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
				Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
			},
			{
				Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
			},
		},
	}
)

func getQueryAndStatsHandler(queryHandler, statsHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loki/api/v1/index/stats" {
			statsHandler.ServeHTTP(w, r)
			return
		}

		if r.URL.Path == "/loki/api/v1/query_range" || r.URL.Path == "/loki/api/v1/query" {
			queryHandler.ServeHTTP(w, r)
			return
		}

		panic("Request not supported")
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
	tpw, stopper, err := NewTripperware(noCacheTestCfg, testEngineOpts, util_log.Logger, l, config.SchemaConfig{
		Configs: testSchemasTSDB,
	}, nil, false, nil)
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

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 0, *queryCount)
	require.Equal(t, 2, *statsCount)

	// testing retry
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	retries, queryHandler := counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	// 3 retries configured.
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)
	rt.Close()

	rt, err = newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	// Configure with cache
	tpw, stopper, err = NewTripperware(testConfig, testEngineOpts, util_log.Logger, l, config.SchemaConfig{
		Configs: testSchemasTSDB,
	}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)

	// testing split interval
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	count, queryHandler := promqlResult(matrix)
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	resp, err := tpw(rt).RoundTrip(req)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)
	lokiResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)

	// testing cache
	count, queryHandler = counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	cacheResp, err := tpw(rt).RoundTrip(req)
	// 0 queries result are cached.
	require.Equal(t, 0, *count)
	require.NoError(t, err)
	lokiCacheResponse, err := LokiCodec.DecodeResponse(ctx, cacheResp, lreq)
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
	tpw, stopper, err := NewTripperware(noCacheTestCfg, testEngineOpts, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil)
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
	_, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	retries, queryHandler := counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.GreaterOrEqual(t, *retries, 3)
	require.Error(t, err)

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 2, *statsCount)
	require.Equal(t, 0, *queryCount)
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
	tpw, stopper, err := NewTripperware(testShardingConfigNoCache, testEngineOpts, util_log.Logger, l, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil)
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
		TimeTs:    testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	// Test MaxQueryBytesRead limit
	statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 2000})
	queryCount, queryHandler := counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 1, *statsCount)
	require.Equal(t, 0, *queryCount)

	// Test MaxQuerierBytesRead limit
	statsCount, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 200})
	queryCount, queryHandler = counter()
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.Equal(t, 2, *statsCount)
	require.Equal(t, 0, *queryCount)

	count, queryHandler := promqlResult(vector)
	_, statsHandler = indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
	rt.setHandler(getQueryAndStatsHandler(queryHandler, statsHandler))
	resp, err := tpw(rt).RoundTrip(req)
	require.Equal(t, 1, *count)
	require.NoError(t, err)

	lokiResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)
	require.IsType(t, &LokiPromResponse{}, lokiResponse)
}

func TestSeriesTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiSeriesRequest{
		Match:   []string{`{job="varlogs"}`},
		StartTs: testTime.Add(-25 * time.Hour), // bigger than split by interval limit
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
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
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

func TestIndexStatsTripperware(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &logproto.IndexStatsRequest{
		Matchers: `{job="varlogs"}`,
		From:     model.TimeFromUnixNano(testTime.Add(-25 * time.Hour).UnixNano()), // bigger than split by interval limit
		Through:  model.TimeFromUnixNano(testTime.UnixNano()),
	}

	ctx := user.InjectOrgID(context.Background(), "1")
	req, err := LokiCodec.EncodeRequest(ctx, lreq)
	require.NoError(t, err)

	req = req.WithContext(ctx)
	err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
	require.NoError(t, err)

	response := logproto.IndexStatsResponse{
		Streams: 100,
		Chunks:  200,
		Bytes:   300,
		Entries: 400,
	}

	count, h := indexStatsResult(response)
	rt.setHandler(h)
	_, err = tpw(rt).RoundTrip(req)
	// 2 queries
	require.Equal(t, 2, *count)
	require.NoError(t, err)

	// Test the cache.
	// It should have the answer already so the query handler shouldn't be hit
	count, h = indexStatsResult(response)
	rt.setHandler(h)
	resp, err := tpw(rt).RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, 0, *count)

	// Test the response is the expected
	indexStatsResponse, err := LokiCodec.DecodeResponse(ctx, resp, lreq)
	require.NoError(t, err)
	res, ok := indexStatsResponse.(*IndexStatsResponse)
	require.Equal(t, true, ok)
	require.Equal(t, response.Streams*2, res.Response.Streams)
	require.Equal(t, response.Chunks*2, res.Response.Chunks)
	require.Equal(t, response.Bytes*2, res.Response.Bytes)
	require.Equal(t, response.Entries*2, res.Response.Entries)
}

func TestNewTripperware_Caches(t *testing.T) {
	for _, tc := range []struct {
		name        string
		config      Config
		numCaches   int
		equalCaches bool
		err         string
	}{
		{
			name: "results cache disabled, stats cache disabled",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: false,
				},
				CacheIndexStatsResults: false,
			},
			numCaches: 0,
			err:       "",
		},
		{
			name: "results cache enabled, stats cache disabled",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: true,
					ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
						CacheConfig: cache.Config{
							EmbeddedCache: cache.EmbeddedCacheConfig{
								Enabled: true,
							},
						},
					},
				},
				CacheIndexStatsResults: false,
			},
			numCaches: 1,
			err:       "",
		},
		{
			name: "results cache enabled, stats cache enabled",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: true,
					ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
						CacheConfig: cache.Config{
							EmbeddedCache: cache.EmbeddedCacheConfig{
								Enabled: true,
							},
						},
					},
				},
				CacheIndexStatsResults: true,
			},
			numCaches:   2,
			equalCaches: true,
			err:         "",
		},
		{
			name: "results cache enabled, stats cache enabled but different",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: true,
					ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
						CacheConfig: cache.Config{
							EmbeddedCache: cache.EmbeddedCacheConfig{
								Enabled:   true,
								MaxSizeMB: 2000,
							},
						},
					},
				},
				CacheIndexStatsResults: true,
				StatsCacheConfig: IndexStatsCacheConfig{
					ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
						CacheConfig: cache.Config{
							EmbeddedCache: cache.EmbeddedCacheConfig{
								Enabled:   true,
								MaxSizeMB: 1000,
							},
						},
					},
				},
			},
			numCaches:   2,
			equalCaches: false,
			err:         "",
		},
		{
			name: "results cache enabled (no config provided)",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: true,
				},
			},
			err: fmt.Sprintf("%s cache is not configured", stats.ResultCache),
		},
		{
			name: "results cache disabled, stats cache enabled (no config provided)",
			config: Config{
				Config: queryrangebase.Config{
					CacheResults: false,
				},
				CacheIndexStatsResults: true,
			},
			numCaches: 0,
			err:       fmt.Sprintf("%s cache is not configured", stats.StatsResultCache),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stopper, err := NewTripperware(tc.config, testEngineOpts, util_log.Logger, fakeLimits{maxQueryLength: 48 * time.Hour, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
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
					caches = append(caches, c)
				}
			}

			require.Equal(t, tc.numCaches, len(caches))

			if tc.numCaches == 2 {
				if tc.equalCaches {
					require.Equal(t, caches[0], caches[1])
				} else {
					require.NotEqual(t, caches[0], caches[1])
				}
			}
		})
	}
}

func TestLogNoFilter(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`,
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
	require.Nil(t, err)
}

func TestRegexpParamsSupport(t *testing.T) {
	l := WithSplitByLimits(fakeLimits{maxSeries: 1, maxQueryParallelism: 2}, 4*time.Hour)
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, l, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`,
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
	req.Body = io.NopCloser(body)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	req = req.WithContext(user.InjectOrgID(context.Background(), "1"))
	require.NoError(t, err)
	_, err = newRoundTripper(
		util_log.Logger,
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected default roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected default roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected metric roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected series roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected labels roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected instant roundtripper called")
			return nil, nil
		}),
		queryrangebase.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected indexStats roundtripper called")
			return nil, nil
		}),
		fakeLimits{},
	).RoundTrip(req)
	require.NoError(t, err)
}

func TestTripperware_EntriesLimit(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{maxEntriesLimitPerQuery: 5000, maxQueryParallelism: 1}, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"}`,
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
			tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
			if stopper != nil {
				defer stopper.Stop()
			}
			require.NoError(t, err)
			rt, err := newfakeRoundTripper()
			require.NoError(t, err)
			defer rt.Close()
			_, h := promqlResult(test.response)
			rt.setHandler(h)

			lreq := &LokiRequest{
				Query:     test.qs,
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

			_, err = tpw(rt).RoundTrip(req)
			if test.expectedError != "" {
				require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, test.expectedError), err)
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
			tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemas}, nil, false, nil)
			if stopper != nil {
				defer stopper.Stop()
			}
			require.NoError(t, err)

			rt, err := newfakeRoundTripper()
			require.NoError(t, err)
			defer rt.Close()
			_, h := promqlResult(tc.response)
			rt.setHandler(h)

			lreq := &LokiRequest{
				Query:     tc.query,
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

			_, err = tpw(rt).RoundTrip(req)
			if tc.expectedError != noErr {
				require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, tc.expectedError), err)
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
			path:       "/prom/query",
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
			path:       "/prom/series",
			expectedOp: SeriesOp,
		},
		{
			name:       "labels_query",
			path:       "/loki/api/v1/labels",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "labels_query_prom",
			path:       "/prom/labels",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_query",
			path:       "/loki/api/v1/label",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "labels_query_prom",
			path:       "/prom/label",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_values_query",
			path:       "/loki/api/v1/label/__name__/values",
			expectedOp: LabelNamesOp,
		},
		{
			name:       "label_values_query_prom",
			path:       "/prom/label/__name__/values",
			expectedOp: LabelNamesOp,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getOperation(tc.path)
			assert.Equal(t, tc.expectedOp, got)
		})
	}
}

type fakeLimits struct {
	maxQueryLength          time.Duration
	maxQueryParallelism     int
	tsdbMaxQueryParallelism int
	maxQueryLookback        time.Duration
	maxEntriesLimitPerQuery int
	maxSeries               int
	splits                  map[string]time.Duration
	minShardingLookback     time.Duration
	queryTimeout            time.Duration
	requiredLabels          []string
	requiredNumberLabels    int
	maxQueryBytesRead       int
	maxQuerierBytesRead     int
	maxStatsCacheFreshness  time.Duration
}

func (f fakeLimits) QuerySplitDuration(key string) time.Duration {
	if f.splits == nil {
		return 0
	}
	return f.splits[key]
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

func (f fakeLimits) RequiredNumberLabels(ctx context.Context, s string) int {
	return f.requiredNumberLabels
}

func (f fakeLimits) MaxStatsCacheFreshness(ctx context.Context, s string) time.Duration {
	return f.maxStatsCacheFreshness
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

func indexStatsResult(v logproto.IndexStatsResponse) (*int, http.Handler) {
	count := 0
	var lock sync.Mutex
	return &count, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if err := marshal.WriteIndexStatsResponseJSON(&v, w); err != nil {
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
