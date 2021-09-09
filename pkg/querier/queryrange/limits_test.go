package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
)

func TestLimits(t *testing.T) {
	l := fakeLimits{
		splits: map[string]time.Duration{"a": time.Minute},
	}

	require.Equal(t, l.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, l.QuerySplitDuration("b"), time.Duration(0))

	wrapped := WithDefaultLimits(l, queryrange.Config{
		SplitQueriesByInterval: time.Hour,
	})

	require.Equal(t, wrapped.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, wrapped.QuerySplitDuration("b"), time.Hour)

	r := &LokiRequest{
		Query:   "qry",
		StartTs: time.Now(),
		Step:    int64(time.Minute / time.Millisecond),
	}

	require.Equal(
		t,
		fmt.Sprintf("%s:%s:%d:%d:%d", "a", r.GetQuery(), r.GetStep(), r.GetStart()/int64(time.Minute/time.Millisecond), int64(time.Minute)),
		cacheKeyLimits{wrapped}.GenerateCacheKey("a", r),
	)
}

func Test_seriesLimiter(t *testing.T) {
	cfg := testConfig
	cfg.SplitQueriesByInterval = time.Hour
	cfg.CacheResults = false
	// split in 7 with 2 in // max.
	tpw, stopper, err := NewTripperware(cfg, util_log.Logger, fakeLimits{maxSeries: 1, maxQueryParallelism: 2}, chunk.SchemaConfig{}, 0, nil)
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
		queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
			return queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
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
		queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
			return queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
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
