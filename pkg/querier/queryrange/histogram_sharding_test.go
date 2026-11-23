package queryrange

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// histogramShardStreams has a few streams of one application, each with a line for every 10 seconds from base, so
// that the shards of a query have a part of the streams each.
func histogramShardStreams(base time.Time) []logproto.Stream {
	var streams []logproto.Stream
	for i := 0; i < 24; i++ {
		s := logproto.Stream{Labels: fmt.Sprintf(`{app="api", pod="%d"}`, i)}
		for sec := 5; sec < 180; sec += 10 {
			s.Entries = append(s.Entries, logproto.Entry{
				Timestamp: base.Add(time.Duration(sec) * time.Second),
				// The values cover a few decades, so that a histogram has many buckets.
				Line: fmt.Sprintf("duration=%g", math.Pow(1.7, float64((i*7+sec)%17))/10),
			})
		}
		streams = append(streams, s)
	}
	return streams
}

// histogramQuerierStandIn answers what the frontend sends to the queriers as a querier does, and what the frontend
// sends and what it gets back go through the protobuf, as they do between two processes. The request comes out as
// the querier reads it: its plan is the one that was serialized, and not the one that the frontend has.
type histogramQuerierStandIn struct {
	t      *testing.T
	engine *logql.QueryEngine

	mu        sync.Mutex
	queries   []string
	shards    []string
	responses []string
}

func (q *histogramQuerierStandIn) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	if _, ok := req.(*logproto.IndexStatsRequest); ok {
		// A lot of data, so that the frontend divides the query into as many shards as it may.
		return &IndexStatsResponse{Response: &logproto.IndexStatsResponse{Bytes: 1 << 40}}, nil
	}

	wrapped, err := DefaultCodec.QueryRequestWrap(ctx, req)
	require.NoError(q.t, err)
	b, err := wrapped.Marshal()
	require.NoError(q.t, err)
	var read QueryRequest
	require.NoError(q.t, read.Unmarshal(b))
	unwrapped, ctx, err := DefaultCodec.QueryRequestUnwrap(ctx, &read)
	require.NoError(q.t, err)

	params, err := ParamsFromRequest(unwrapped)
	require.NoError(q.t, err)

	res, err := q.engine.Query(params).Exec(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := ResultToResponse(res, params)
	require.NoError(q.t, err)

	q.mu.Lock()
	q.queries = append(q.queries, params.GetExpression().String())
	q.shards = append(q.shards, fmt.Sprint(params.Shards()))
	q.responses = append(q.responses, fmt.Sprintf("%T", resp))
	q.mu.Unlock()

	answer, err := QueryResponseWrap(resp)
	require.NoError(q.t, err)
	b, err = answer.Marshal()
	require.NoError(q.t, err)
	var readAnswer QueryResponse
	require.NoError(q.t, readAnswer.Unmarshal(b))
	return QueryResponseUnwrap(&readAnswer)
}

func requireSameHistogramResult(t *testing.T, want, got promql_parser.Value) {
	t.Helper()
	switch w := want.(type) {
	case promql.Matrix:
		g, ok := got.(promql.Matrix)
		require.True(t, ok, "%T", got)
		require.Len(t, g, len(w))
		sort.Sort(w)
		sort.Sort(g)
		for i := range w {
			require.Equal(t, w[i].Metric, g[i].Metric)
			require.Equal(t, w[i].Floats, g[i].Floats, "series %s", w[i].Metric)
		}
	case promql.Vector:
		g, ok := got.(promql.Vector)
		require.True(t, ok, "%T", got)
		require.Len(t, g, len(w))
		sort.Slice(w, func(i, j int) bool { return w[i].Metric.String() < w[j].Metric.String() })
		sort.Slice(g, func(i, j int) bool { return g[i].Metric.String() < g[j].Metric.String() })
		for i := range w {
			require.Equal(t, w[i].Metric, g[i].Metric)
			require.Equal(t, w[i].F, g[i].F, "series %s", w[i].Metric)
		}
	default:
		require.Failf(t, "unexpected result", "%T", want)
	}
}

// A sharded histogram query is divided in the frontend, the parts that the shards run go to the queriers as a plan
// whose root is a histogram, and what they return is a histogram, which the frontend adds up and reads. The result
// is that of the query that is not sharded.
func TestShardedHistograms_throughTheFrontend(t *testing.T) {
	const rng = `{app="api"} | logfmt | unwrap duration`

	ctx := user.InjectOrgID(context.Background(), "1")
	opts := logql.EngineOpts{MaxLookBackPeriod: 30 * time.Second, EnableHistograms: true}
	limits := fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 4, tsdbMaxQueryParallelism: 4, queryTimeout: time.Minute}
	streams := histogramShardStreams(time.Unix(0, 0))

	requests := func(query string) map[string]queryrangebase.Request {
		return map[string]queryrangebase.Request{
			"range": &LokiRequest{
				Query:     query,
				Limit:     1000,
				Step:      60_000,
				StartTs:   time.Unix(60, 0),
				EndTs:     time.Unix(180, 0),
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query_range",
				Plan:      testutil.MustPlan(query),
			},
			"instant": &LokiInstantRequest{
				Query:     query,
				Limit:     1000,
				TimeTs:    time.Unix(120, 0),
				Direction: logproto.FORWARD,
				Path:      "/loki/api/v1/query",
				Plan:      testutil.MustPlan(query),
			},
		}
	}

	for name, query := range map[string]string{
		"a quantile of a sum":        `histogram_quantile(0.9, sum by (app) (histogram_over_time(` + rng + ` [1m])))`,
		"a count without a grouping": `histogram_count(sum(histogram_over_time(` + rng + ` [1m])))`,
		"a grouping of the range":    `histogram_fraction(0.1, 5, histogram_over_time(` + rng + ` [1m]) by (app))`,
		"custom buckets":             `histogram_fraction(0.5, 2, sum(histogram_over_time(buckets(0.1, 0.5, 1, 2, 5), ` + rng + ` [1m])))`,
	} {
		for kind, req := range requests(query) {
			// What the engine returns for the query without sharding is what the frontend has to return.
			params, err := ParamsFromRequest(req)
			require.NoError(t, err)
			want, err := logql.NewEngine(opts, logql.NewMockQuerier(0, streams), logql.NoLimits, log.NewNopLogger()).Query(params).Exec(ctx)
			require.NoError(t, err)

			for _, enabled := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/sharded=%t", name, kind, enabled), func(t *testing.T) {
					querier := &histogramQuerierStandIn{
						t:      t,
						engine: logql.NewEngine(opts, logql.NewMockQuerier(2, streams), logql.NoLimits, log.NewNopLogger()),
					}
					cfg := Config{}
					if enabled {
						cfg.ShardAggregations = []string{logql.ShardHistogramOverTime}
					}
					mware := newASTMapperware(
						[]config.PeriodConfig{{IndexType: types.IndexTypeTSDB}},
						opts,
						querier,
						querier,
						querier,
						log.NewNopLogger(),
						nilShardingMetrics,
						cfg,
						limits,
						2,
					)

					resp, err := mware.Do(ctx, req)
					require.NoError(t, err)
					got, err := ResponseToResult(resp)
					require.NoError(t, err)
					requireSameHistogramResult(t, want.Data, got.Data)

					querier.mu.Lock()
					defer querier.mu.Unlock()
					if !enabled {
						require.Equal(t, []string{params.GetExpression().String()}, querier.queries, "the query went to a querier as it is")
						return
					}

					// Each shard ran the part of the query that is the histogram, which a query cannot be, and a
					// histogram came back from it.
					require.Len(t, querier.queries, 2, "a part for each shard")
					require.ElementsMatch(t, []string{"[0_of_2]", "[1_of_2]"}, querier.shards)
					for i, query := range querier.queries {
						expr, err := syntax.ParseExprWithoutValidation(query)
						require.NoError(t, err)
						sample, ok := expr.(syntax.SampleExpr)
						require.True(t, ok)
						require.True(t, syntax.IsHistogramValued(sample), "the query of a shard returns a histogram: %s", query)
						require.Equal(t, "*queryrange.HistogramResponse", querier.responses[i])
					}
				})
			}
		}
	}
}

// A query that has a part that is histograms and a part that is not is divided part by part, and a part that is not
// enabled is run by a querier as a whole.
func TestShardedHistograms_throughTheFrontendNextToAShardedPart(t *testing.T) {
	const query = `histogram_count(sum(histogram_over_time({app="api"} | logfmt | unwrap duration [1m]))) / sum(count_over_time({app="api"} [1m]))`

	ctx := user.InjectOrgID(context.Background(), "1")
	opts := logql.EngineOpts{MaxLookBackPeriod: 30 * time.Second, EnableHistograms: true}
	streams := histogramShardStreams(time.Unix(0, 0))

	req := &LokiInstantRequest{
		Query:     query,
		Limit:     1000,
		TimeTs:    time.Unix(120, 0),
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan:      testutil.MustPlan(query),
	}
	params, err := ParamsFromRequest(req)
	require.NoError(t, err)
	want, err := logql.NewEngine(opts, logql.NewMockQuerier(0, streams), logql.NoLimits, log.NewNopLogger()).Query(params).Exec(ctx)
	require.NoError(t, err)

	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("sharded=%t", enabled), func(t *testing.T) {
			querier := &histogramQuerierStandIn{
				t:      t,
				engine: logql.NewEngine(opts, logql.NewMockQuerier(2, streams), logql.NoLimits, log.NewNopLogger()),
			}
			cfg := Config{}
			if enabled {
				cfg.ShardAggregations = []string{logql.ShardHistogramOverTime}
			}
			mware := newASTMapperware(
				[]config.PeriodConfig{{IndexType: types.IndexTypeTSDB}},
				opts,
				querier,
				querier,
				querier,
				log.NewNopLogger(),
				nilShardingMetrics,
				cfg,
				fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 4, tsdbMaxQueryParallelism: 4, queryTimeout: time.Minute},
				2,
			)

			resp, err := mware.Do(ctx, req)
			require.NoError(t, err)
			got, err := ResponseToResult(resp)
			require.NoError(t, err)
			requireSameHistogramResult(t, want.Data, got.Data)

			querier.mu.Lock()
			defer querier.mu.Unlock()
			var histograms, whole, others int
			for _, q := range querier.queries {
				expr, err := syntax.ParseExprWithoutValidation(q)
				require.NoError(t, err)
				switch sample := expr.(syntax.SampleExpr); {
				case syntax.IsHistogramValued(sample):
					histograms++
				case syntax.UsesHistograms(sample):
					whole++
				default:
					others++
				}
			}
			require.Equal(t, 2, others, "the part that is not histograms is sharded, enabled or not")
			if enabled {
				require.Equal(t, 2, histograms, "a part for each shard")
				require.Zero(t, whole)
			} else {
				require.Zero(t, histograms)
				require.Equal(t, 1, whole, "the histogram part is run by a querier as a whole")
			}
		})
	}
}

// The whole chain of the query frontend, with the middleware that comes after the sharding too: the parts that the
// shards run go through all of it on their way to a querier, and none of it may refuse or change a query whose root is
// a histogram.
func TestShardedHistograms_inTheTripperware(t *testing.T) {
	const query = `histogram_quantile(0.9, sum by (app) (histogram_over_time({app="api"} | logfmt | unwrap duration [1m])))`

	ctx := user.InjectOrgID(context.Background(), "1")
	opts := logql.EngineOpts{MaxLookBackPeriod: 30 * time.Second, EnableHistograms: true}
	limits := fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 4, tsdbMaxQueryParallelism: 4, queryTimeout: time.Minute}
	base := testTime.Add(-time.Hour).Truncate(time.Minute)
	streams := histogramShardStreams(base)

	requests := map[string]queryrangebase.Request{
		"range": &LokiRequest{
			Query:     query,
			Limit:     1000,
			Step:      60_000,
			StartTs:   base.Add(time.Minute),
			EndTs:     base.Add(3 * time.Minute),
			Direction: logproto.FORWARD,
			Path:      "/loki/api/v1/query_range",
			Plan:      testutil.MustPlan(query),
		},
		"instant": &LokiInstantRequest{
			Query:     query,
			Limit:     1000,
			TimeTs:    base.Add(2 * time.Minute),
			Direction: logproto.FORWARD,
			Path:      "/loki/api/v1/query",
			Plan:      testutil.MustPlan(query),
		},
	}

	for kind, req := range requests {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/sharded=%t", kind, enabled), func(t *testing.T) {
				params, err := ParamsFromRequest(req)
				require.NoError(t, err)
				want, err := logql.NewEngine(opts, logql.NewMockQuerier(0, streams), logql.NoLimits, log.NewNopLogger()).Query(params).Exec(ctx)
				require.NoError(t, err)

				cfg := testConfig
				cfg.ShardedQueries = true
				cfg.CacheResults = false
				cfg.CacheIndexStatsResults = false
				cfg.CacheInstantMetricResults = false
				cfg.ShardAggregations = nil
				if enabled {
					cfg.ShardAggregations = []string{logql.ShardHistogramOverTime}
				}

				tpw, stopper, err := NewMiddleware(cfg, opts, nil, util_log.Logger, limits, config.SchemaConfig{Configs: testSchemasTSDB}, nil, false, nil, constants.Loki)
				if stopper != nil {
					defer stopper.Stop()
				}
				require.NoError(t, err)

				querier := &histogramQuerierStandIn{
					t:      t,
					engine: logql.NewEngine(opts, logql.NewMockQuerier(2, streams), logql.NoLimits, log.NewNopLogger()),
				}
				resp, err := tpw.Wrap(querier).Do(ctx, req)
				require.NoError(t, err)
				got, err := ResponseToResult(resp)
				require.NoError(t, err)
				requireSameHistogramResult(t, want.Data, got.Data)

				querier.mu.Lock()
				defer querier.mu.Unlock()
				if !enabled {
					require.Len(t, querier.queries, 1, "the query went to a querier as a whole")
					return
				}
				require.GreaterOrEqual(t, len(querier.queries), 2, "the query was divided into shards")
				for i, q := range querier.queries {
					expr, err := syntax.ParseExprWithoutValidation(q)
					require.NoError(t, err)
					require.True(t, syntax.IsHistogramValued(expr.(syntax.SampleExpr)), "%s", q)
					require.Equal(t, "*queryrange.HistogramResponse", querier.responses[i])
				}
			})
		}
	}
}
