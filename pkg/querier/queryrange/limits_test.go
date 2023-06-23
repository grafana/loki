package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/math"
)

func TestLimits(t *testing.T) {
	l := fakeLimits{
		splits: map[string]time.Duration{"a": time.Minute},
	}

	wrapped := WithSplitByLimits(l, time.Hour)

	// Test default
	require.Equal(t, wrapped.QuerySplitDuration("b"), time.Hour)
	// Ensure we override the underlying implementation
	require.Equal(t, wrapped.QuerySplitDuration("a"), time.Hour)

	r := &LokiRequest{
		Query:   "qry",
		StartTs: time.Now(),
		Step:    int64(time.Minute / time.Millisecond),
	}

	require.Equal(
		t,
		fmt.Sprintf("%s:%s:%d:%d:%d", "a", r.GetQuery(), r.GetStep(), r.GetStart()/int64(time.Hour/time.Millisecond), int64(time.Hour)),
		cacheKeyLimits{wrapped, nil}.GenerateCacheKey(context.Background(), "a", r),
	)
}

func Test_seriesLimiter(t *testing.T) {
	cfg := testConfig
	cfg.CacheResults = false
	cfg.CacheIndexStatsResults = false
	// split in 7 with 2 in // max.
	l := WithSplitByLimits(fakeLimits{maxSeries: 1, maxQueryParallelism: 2}, time.Hour)
	tpw, stopper, err := NewTripperware(cfg, testEngineOpts, util_log.Logger, l, config.SchemaConfig{
		Configs: testSchemas,
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
	defer rt.Close()

	count, h := promqlResult(matrix)
	rt.setHandler(h)

	_, err = tpw(rt).RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, 7, *count)

	// 2 series should not be allowed.
	c := new(int)
	m := &sync.Mutex{}
	h = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		m.Lock()
		defer m.Unlock()
		defer func() {
			*c++
		}()
		// first time returns  a single series
		if *c == 0 {
			if err := marshal.WriteQueryResponseJSON(logqlmodel.Result{Data: matrix}, rw); err != nil {
				panic(err)
			}
			return
		}
		// second time returns a different series.
		if err := marshal.WriteQueryResponseJSON(logqlmodel.Result{
			Data: promql.Matrix{
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
							Value: "anotherjob",
						},
					},
				},
			},
		}, rw); err != nil {
			panic(err)
		}
	})
	rt.setHandler(h)

	_, err = tpw(rt).RoundTrip(req)
	require.Error(t, err)
	require.LessOrEqual(t, *c, 4)
}

func Test_MaxQueryParallelism(t *testing.T) {
	maxQueryParallelism := 2
	f, err := newfakeRoundTripper()
	require.Nil(t, err)
	var count atomic.Int32
	var max atomic.Int32
	f.setHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		cur := count.Inc()
		if cur > max.Load() {
			max.Store(cur)
		}
		defer count.Dec()
		// simulate some work
		time.Sleep(20 * time.Millisecond)
	}))
	ctx := user.InjectOrgID(context.Background(), "foo")

	r, err := http.NewRequestWithContext(ctx, "GET", "/query_range", http.NoBody)
	require.Nil(t, err)

	_, _ = NewLimitedRoundTripper(f, LokiCodec, fakeLimits{maxQueryParallelism: maxQueryParallelism},
		testSchemas,
		queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
			return queryrangebase.HandlerFunc(func(c context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				var wg sync.WaitGroup
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, _ = next.Do(c, &LokiRequest{})
					}()
				}
				wg.Wait()
				return nil, nil
			})
		}),
	).RoundTrip(r)
	maxFound := int(max.Load())
	require.LessOrEqual(t, maxFound, maxQueryParallelism, "max query parallelism: ", maxFound, " went over the configured one:", maxQueryParallelism)
}

func Test_MaxQueryParallelismLateScheduling(t *testing.T) {
	maxQueryParallelism := 2
	f, err := newfakeRoundTripper()
	require.Nil(t, err)

	f.setHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// simulate some work
		time.Sleep(20 * time.Millisecond)
	}))
	ctx := user.InjectOrgID(context.Background(), "foo")

	r, err := http.NewRequestWithContext(ctx, "GET", "/query_range", http.NoBody)
	require.Nil(t, err)

	_, _ = NewLimitedRoundTripper(f, LokiCodec, fakeLimits{maxQueryParallelism: maxQueryParallelism},
		testSchemas,
		queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
			return queryrangebase.HandlerFunc(func(c context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				for i := 0; i < 10; i++ {
					go func() {
						_, _ = next.Do(c, &LokiRequest{})
					}()
				}
				return nil, nil
			})
		}),
	).RoundTrip(r)
}

func Test_MaxQueryParallelismDisable(t *testing.T) {
	maxQueryParallelism := 0
	f, err := newfakeRoundTripper()
	require.Nil(t, err)

	f.setHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// simulate some work
		time.Sleep(20 * time.Millisecond)
	}))
	ctx := user.InjectOrgID(context.Background(), "foo")

	r, err := http.NewRequestWithContext(ctx, "GET", "/query_range", http.NoBody)
	require.Nil(t, err)

	_, err = NewLimitedRoundTripper(f, LokiCodec, fakeLimits{maxQueryParallelism: maxQueryParallelism},
		testSchemas,
		queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
			return queryrangebase.HandlerFunc(func(c context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				for i := 0; i < 10; i++ {
					go func() {
						_, _ = next.Do(c, &LokiRequest{})
					}()
				}
				return nil, nil
			})
		}),
	).RoundTrip(r)
	require.Error(t, err)
}

func Test_MaxQueryLookBack(t *testing.T) {
	tpw, stopper, err := NewTripperware(testConfig, testEngineOpts, util_log.Logger, fakeLimits{
		maxQueryLookback:    1 * time.Hour,
		maxQueryParallelism: 1,
	}, config.SchemaConfig{
		Configs: testSchemas,
	}, nil, false, nil)
	if stopper != nil {
		defer stopper.Stop()
	}
	require.NoError(t, err)
	rt, err := newfakeRoundTripper()
	require.NoError(t, err)
	defer rt.Close()

	lreq := &LokiRequest{
		Query:     `{app="foo"} |= "foo"`,
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

func Test_GenerateCacheKey_NoDivideZero(t *testing.T) {
	l := cacheKeyLimits{WithSplitByLimits(nil, 0), nil}
	start := time.Now()
	r := &LokiRequest{
		Query:   "qry",
		StartTs: start,
		Step:    int64(time.Minute / time.Millisecond),
	}

	require.Equal(
		t,
		fmt.Sprintf("foo:qry:%d:0:0", r.GetStep()),
		l.GenerateCacheKey(context.Background(), "foo", r),
	)
}

func Test_WeightedParallelism(t *testing.T) {
	limits := &fakeLimits{
		tsdbMaxQueryParallelism: 100,
		maxQueryParallelism:     10,
	}

	for _, cfgs := range []struct {
		desc    string
		periods string
	}{
		{
			desc: "end configs",
			periods: `
- from: "2022-01-01"
  store: boltdb-shipper
  object_store: gcs
  schema: v12
- from: "2022-01-02"
  store: tsdb
  object_store: gcs
  schema: v12
`,
		},
		{
			// Add another test that wraps the tested period configs with other unused configs
			// to ensure we bounds-test properly
			desc: "middle configs",
			periods: `
- from: "2021-01-01"
  store: boltdb-shipper
  object_store: gcs
  schema: v12
- from: "2022-01-01"
  store: boltdb-shipper
  object_store: gcs
  schema: v12
- from: "2022-01-02"
  store: tsdb
  object_store: gcs
  schema: v12
- from: "2023-01-02"
  store: tsdb
  object_store: gcs
  schema: v12
`,
		},
	} {
		var confs []config.PeriodConfig
		require.Nil(t, yaml.Unmarshal([]byte(cfgs.periods), &confs))
		parsed, err := time.Parse("2006-01-02", "2022-01-02")
		borderTime := model.TimeFromUnix(parsed.Unix())
		require.Nil(t, err)

		for _, tc := range []struct {
			desc       string
			start, end model.Time
			exp        int
		}{
			{
				desc:  "50% each",
				start: borderTime.Add(-time.Hour),
				end:   borderTime.Add(time.Hour),
				exp:   55,
			},
			{
				desc:  "75/25 split",
				start: borderTime.Add(-3 * time.Hour),
				end:   borderTime.Add(time.Hour),
				exp:   32,
			},
			{
				desc:  "start==end",
				start: borderTime.Add(time.Hour),
				end:   borderTime.Add(time.Hour),
				exp:   100,
			},
		} {
			t.Run(cfgs.desc+tc.desc, func(t *testing.T) {
				require.Equal(t, tc.exp, WeightedParallelism(context.Background(), confs, "fake", limits, tc.start, tc.end))
			})
		}
	}

}

func Test_WeightedParallelism_DivideByZeroError(t *testing.T) {
	t.Run("query end before start", func(t *testing.T) {
		parsed, err := time.Parse("2006-01-02", "2022-01-02")
		require.NoError(t, err)
		borderTime := model.TimeFromUnix(parsed.Unix())

		confs := []config.PeriodConfig{
			{
				From: config.DayTime{
					Time: borderTime.Add(-1 * time.Hour),
				},
				IndexType: config.TSDBType,
			},
		}

		result := WeightedParallelism(context.Background(), confs, "fake", &fakeLimits{tsdbMaxQueryParallelism: 50}, borderTime, borderTime.Add(-1*time.Hour))
		require.Equal(t, 1, result)
	})

	t.Run("negative start and end time", func(t *testing.T) {
		parsed, err := time.Parse("2006-01-02", "2022-01-02")
		require.NoError(t, err)
		borderTime := model.TimeFromUnix(parsed.Unix())

		confs := []config.PeriodConfig{
			{
				From: config.DayTime{
					Time: borderTime.Add(-1 * time.Hour),
				},
				IndexType: config.TSDBType,
			},
		}

		result := WeightedParallelism(context.Background(), confs, "fake", &fakeLimits{maxQueryParallelism: 50}, -100, -50)
		require.Equal(t, 1, result)
	})

	t.Run("query start and end time before config start", func(t *testing.T) {
		parsed, err := time.Parse("2006-01-02", "2022-01-02")
		require.NoError(t, err)
		borderTime := model.TimeFromUnix(parsed.Unix())

		confs := []config.PeriodConfig{
			{
				From: config.DayTime{
					Time: borderTime.Add(-1 * time.Hour),
				},
				IndexType: config.TSDBType,
			},
		}

		result := WeightedParallelism(context.Background(), confs, "fake", &fakeLimits{maxQueryParallelism: 50}, confs[0].From.Add(-24*time.Hour), confs[0].From.Add(-12*time.Hour))
		require.Equal(t, 1, result)
	})
}

func getFakeStatsHandler(retBytes uint64) (queryrangebase.Handler, *int, error) {
	fakeRT, err := newfakeRoundTripper()
	if err != nil {
		return nil, nil, err
	}

	count, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: retBytes})

	fakeRT.setHandler(statsHandler)

	return queryrangebase.NewRoundTripperHandler(fakeRT, LokiCodec), count, nil
}

func Test_MaxQuerySize(t *testing.T) {
	const statsBytes = 1000

	schemas := []config.PeriodConfig{
		{
			// BoltDB -> Time -4 days
			From:      config.DayTime{Time: model.TimeFromUnix(testTime.Add(-96 * time.Hour).Unix())},
			IndexType: config.BoltDBShipperType,
		},
		{
			// TSDB -> Time -2 days
			From:      config.DayTime{Time: model.TimeFromUnix(testTime.Add(-48 * time.Hour).Unix())},
			IndexType: config.TSDBType,
		},
	}

	for _, tc := range []struct {
		desc       string
		schema     string
		query      string
		queryRange time.Duration
		queryStart time.Time
		queryEnd   time.Time
		limits     Limits

		shouldErr                bool
		expectedQueryStatsHits   int
		expectedQuerierStatsHits int
	}{
		{
			desc:       "No TSDB",
			schema:     config.BoltDBShipperType,
			query:      `{app="foo"} |= "foo"`,
			queryRange: 1 * time.Hour,

			queryStart: testTime.Add(-96 * time.Hour),
			queryEnd:   testTime.Add(-90 * time.Hour),
			limits: fakeLimits{
				maxQueryBytesRead:   1,
				maxQuerierBytesRead: 1,
			},

			shouldErr:                false,
			expectedQueryStatsHits:   0,
			expectedQuerierStatsHits: 0,
		},
		{
			desc:       "Unlimited",
			query:      `{app="foo"} |= "foo"`,
			queryStart: testTime.Add(-48 * time.Hour),
			queryEnd:   testTime,
			limits: fakeLimits{
				maxQueryBytesRead:   0,
				maxQuerierBytesRead: 0,
			},

			shouldErr:                false,
			expectedQueryStatsHits:   0,
			expectedQuerierStatsHits: 0,
		},
		{
			desc:       "1 hour range",
			query:      `{app="foo"} |= "foo"`,
			queryStart: testTime.Add(-1 * time.Hour),
			queryEnd:   testTime,
			limits: fakeLimits{
				maxQueryBytesRead:   statsBytes,
				maxQuerierBytesRead: statsBytes,
			},

			shouldErr: false,
			// [testTime-1h, testTime)
			expectedQueryStatsHits:   1,
			expectedQuerierStatsHits: 1,
		},
		{
			desc:       "Query size too big",
			query:      `{app="foo"} |= "foo"`,
			queryStart: testTime.Add(-1 * time.Hour),
			queryEnd:   testTime,
			limits: fakeLimits{
				maxQueryBytesRead:   statsBytes - 1,
				maxQuerierBytesRead: statsBytes,
			},

			shouldErr:                true,
			expectedQueryStatsHits:   1,
			expectedQuerierStatsHits: 0,
		},
		{
			desc:       "Querier size too big",
			query:      `{app="foo"} |= "foo"`,
			queryStart: testTime.Add(-1 * time.Hour),
			queryEnd:   testTime,
			limits: fakeLimits{
				maxQueryBytesRead:   statsBytes,
				maxQuerierBytesRead: statsBytes - 1,
			},

			shouldErr:                true,
			expectedQueryStatsHits:   1,
			expectedQuerierStatsHits: 1,
		},
		{
			desc:       "Multi-matchers with offset",
			query:      `sum_over_time ({app="foo"} |= "foo" | unwrap foo [5m] ) / sum_over_time ({app="bar"} |= "bar" | unwrap bar [5m] offset 1h)`,
			queryStart: testTime.Add(-1 * time.Hour),
			queryEnd:   testTime,
			limits: fakeLimits{
				maxQueryBytesRead:   statsBytes,
				maxQuerierBytesRead: statsBytes,
			},

			shouldErr: false,
			// *2 since we have two matcher groups
			expectedQueryStatsHits:   1 * 2,
			expectedQuerierStatsHits: 1 * 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			queryStatsHandler, queryStatsHits, err := getFakeStatsHandler(uint64(statsBytes / math.Max(tc.expectedQueryStatsHits, 1)))
			require.NoError(t, err)

			querierStatsHandler, querierStatsHits, err := getFakeStatsHandler(uint64(statsBytes / math.Max(tc.expectedQuerierStatsHits, 1)))
			require.NoError(t, err)

			fakeRT, err := newfakeRoundTripper()
			require.NoError(t, err)

			_, promHandler := promqlResult(matrix)
			fakeRT.setHandler(promHandler)

			lokiReq := &LokiRequest{
				Query:     tc.query,
				Limit:     1000,
				StartTs:   tc.queryStart,
				EndTs:     tc.queryEnd,
				Direction: logproto.FORWARD,
				Path:      "/query_range",
			}

			ctx := user.InjectOrgID(context.Background(), "foo")
			req, err := LokiCodec.EncodeRequest(ctx, lokiReq)
			require.NoError(t, err)

			req = req.WithContext(ctx)
			err = user.InjectOrgIDIntoHTTPRequest(ctx, req)
			require.NoError(t, err)

			middlewares := []queryrangebase.Middleware{
				NewQuerySizeLimiterMiddleware(schemas, testEngineOpts, util_log.Logger, tc.limits, queryStatsHandler),
				NewQuerierSizeLimiterMiddleware(schemas, testEngineOpts, util_log.Logger, tc.limits, querierStatsHandler),
			}

			_, err = queryrangebase.NewRoundTripper(fakeRT, LokiCodec, nil, middlewares...).RoundTrip(req)

			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedQueryStatsHits, *queryStatsHits)
			require.Equal(t, tc.expectedQuerierStatsHits, *querierStatsHits)
		})
	}

}

func Test_MaxQuerySize_MaxLookBackPeriod(t *testing.T) {
	engineOpts := testEngineOpts
	engineOpts.MaxLookBackPeriod = 1 * time.Hour

	lim := fakeLimits{
		maxQueryBytesRead:   1 << 10,
		maxQuerierBytesRead: 1 << 10,
	}

	statsHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		// This is the actual check that we're testing.
		require.Equal(t, testTime.Add(-engineOpts.MaxLookBackPeriod).UnixMilli(), req.GetStart())

		return &IndexStatsResponse{
			Response: &logproto.IndexStatsResponse{
				Bytes: 1 << 10,
			},
		}, nil
	})

	for _, tc := range []struct {
		desc       string
		middleware queryrangebase.Middleware
	}{
		{
			desc:       "QuerySizeLimiter",
			middleware: NewQuerySizeLimiterMiddleware(testSchemasTSDB, engineOpts, util_log.Logger, lim, statsHandler),
		},
		{
			desc:       "QuerierSizeLimiter",
			middleware: NewQuerierSizeLimiterMiddleware(testSchemasTSDB, engineOpts, util_log.Logger, lim, statsHandler),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			lokiReq := &LokiInstantRequest{
				Query:     `{cluster="dev-us-central-0"}`,
				Limit:     1000,
				TimeTs:    testTime,
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query",
			}

			handler := tc.middleware.Wrap(
				queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					return &LokiResponse{}, nil
				}),
			)

			ctx := user.InjectOrgID(context.Background(), "foo")
			_, err := handler.Do(ctx, lokiReq)
			require.NoError(t, err)
		})
	}
}
