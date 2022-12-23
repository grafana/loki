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
	// split in 7 with 2 in // max.
	l := WithSplitByLimits(fakeLimits{maxSeries: 1, maxQueryParallelism: 2}, time.Hour)
	tpw, stopper, err := NewTripperware(cfg, util_log.Logger, l, config.SchemaConfig{
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
	tpw, stopper, err := NewTripperware(testConfig, util_log.Logger, fakeLimits{
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
