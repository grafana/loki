package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
)

func TestLimits(t *testing.T) {
	l := fakeLimits{
		splits: map[string]time.Duration{"a": time.Minute},
	}

	require.Equal(t, l.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, l.QuerySplitDuration("b"), time.Duration(0))

	wrapped := WithDefaultLimits(l, queryrange.Config{
		SplitQueriesByDay: true,
	})

	require.Equal(t, wrapped.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, wrapped.QuerySplitDuration("b"), 24*time.Hour)

	wrapped = WithDefaultLimits(l, queryrange.Config{
		SplitQueriesByDay:      true, // should be overridden by SplitQueriesByInterval
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
	// split in 6 with 4 in // max.
	tpw, stopper, err := NewTripperware(cfg, util_log.Logger, fakeLimits{maxSeries: 1, maxQueryParallelism: 2}, chunk.SchemaConfig{}, 0, nil)
	if stopper != nil {
		defer stopper.Stop()
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

	count, h := promqlResult(matrix)
	rt.setHandler(h)

	_, err = tpw(rt).RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, 6, *count)

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
			if err := marshal.WriteQueryResponseJSON(logql.Result{Data: matrix}, rw); err != nil {
				panic(err)
			}
			return
		}
		// second time returns a different series.
		if err := marshal.WriteQueryResponseJSON(logql.Result{
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
