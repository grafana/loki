package queryrange

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/server"
)

const (
	histogramGateRange = `{app="a"} | logfmt | unwrap duration [1m]`
	histogramGateFlag  = "querier.engine.enable-histograms"
)

var histogramGateQueries = []struct {
	name        string
	query       string
	wantRefused bool
}{
	{name: "quantile of a sum", query: `histogram_quantile(0.9, sum by (a) (histogram_over_time(` + histogramGateRange + `)))`, wantRefused: true},
	// The range aggregation after the histogram must not hide it.
	{name: "before a range aggregation", query: `histogram_count(histogram_over_time(` + histogramGateRange + `)) + rate({app="a"}[1m])`, wantRefused: true},
	{name: "no histogram", query: `sum by (app) (rate({app="a"}[1m]))`},
}

func histogramGateRequests(query string) map[string]queryrangebase.Request {
	return map[string]queryrangebase.Request{
		"range": &LokiRequest{
			Query:     query,
			Limit:     1000,
			Step:      30000, // 30sec
			StartTs:   testTime.Add(-6 * time.Hour),
			EndTs:     testTime,
			Direction: logproto.FORWARD,
			Path:      "/query_range",
			Plan:      testutil.MustPlan(query),
		},
		"instant": &LokiInstantRequest{
			Query:     query,
			Limit:     1000,
			TimeTs:    testTime,
			Direction: logproto.FORWARD,
			Path:      "/loki/api/v1/query",
			Plan:      testutil.MustPlan(query),
		},
	}
}

func TestHistogramGateMiddleware(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")

	for _, tc := range histogramGateQueries {
		for kind, req := range histogramGateRequests(tc.query) {
			t.Run(tc.name+"/"+kind+"/off", func(t *testing.T) {
				calls, next := counter()
				_, err := NewHistogramGateMiddleware(logql.EngineOpts{}).Wrap(next).Do(ctx, req)

				if !tc.wantRefused {
					require.NoError(t, err)
					require.Equal(t, 1, *calls)
					return
				}
				require.Error(t, err)
				// It is a client error, so the frontend does not retry it, and it says what to do.
				require.ErrorIs(t, err, logqlerr.ErrParse)
				require.ErrorContains(t, err, "-"+histogramGateFlag)
				status, _ := server.ClientHTTPStatusAndError(err)
				require.Equal(t, http.StatusBadRequest, status)
				require.Zero(t, *calls, "a query that is refused is not passed on")
			})

			t.Run(tc.name+"/"+kind+"/on", func(t *testing.T) {
				calls, next := counter()
				_, err := NewHistogramGateMiddleware(logql.EngineOpts{EnableHistograms: true}).Wrap(next).Do(ctx, req)
				require.NoError(t, err)
				require.Equal(t, 1, *calls)
			})
		}
	}
}

// A request that the gate cannot judge is passed on. The engine reports what is wrong with it.
func TestHistogramGateMiddleware_PassesOnWhatItCannotJudge(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	histogramQuery := histogramGateQueries[0].query

	for name, req := range map[string]queryrangebase.Request{
		"a range request without a plan":    &LokiRequest{Query: histogramQuery},
		"an instant request without a plan": &LokiInstantRequest{Query: histogramQuery},
		"a request that is not a query":     &LokiSeriesRequest{Match: []string{`{app="a"}`}},
	} {
		t.Run(name, func(t *testing.T) {
			calls, next := counter()
			_, err := NewHistogramGateMiddleware(logql.EngineOpts{}).Wrap(next).Do(ctx, req)
			require.NoError(t, err)
			require.Equal(t, 1, *calls)
		})
	}
}

// The gate is in the middleware of the frontend, so that a query is refused before it is split, looked up in the
// index, or sent to a querier. This runs the whole tripperware, with and without sharding: sharding is the only
// thing that makes the frontend evaluate a histogram query, and the gate must not depend on it.
func TestHistogramGate_InTheTripperware(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	limits := fakeLimits{
		maxSeries:               math.MaxInt32,
		maxQueryParallelism:     1,
		tsdbMaxQueryParallelism: 1,
		maxQueryBytesRead:       1000,
		maxQuerierBytesRead:     100,
	}

	for _, sharded := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("sharded=%t/enabled=%t", sharded, enabled), func(t *testing.T) {
				cfg := testConfig
				cfg.ShardedQueries = sharded
				cfg.CacheResults = false
				cfg.CacheIndexStatsResults = false
				cfg.CacheInstantMetricResults = false
				engineOpts := testEngineOpts
				engineOpts.EnableHistograms = enabled

				tpw, stopper, err := NewMiddleware(cfg, engineOpts, nil, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
				if stopper != nil {
					defer stopper.Stop()
				}
				require.NoError(t, err)

				for kind, req := range histogramGateRequests(histogramGateQueries[0].query) {
					t.Run(kind, func(t *testing.T) {
						statsCount, statsHandler := indexStatsResult(logproto.IndexStatsResponse{Bytes: 10})
						queryCount, queryHandler := counter()
						_, err := tpw.Wrap(getQueryAndStatsHandler(queryHandler, statsHandler)).Do(ctx, req)

						if enabled {
							require.NoError(t, err)
							require.Positive(t, *queryCount, "a query that is not refused reaches the querier")
							return
						}
						require.ErrorIs(t, err, logqlerr.ErrParse)
						require.ErrorContains(t, err, "-"+histogramGateFlag)
						require.Zero(t, *statsCount, "a query that is refused does not look up the index")
						require.Zero(t, *queryCount, "a query that is refused does not reach the querier")
					})
				}
			})
		}
	}
}
