package logql

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const histogramShardRange = `{app="api"} | json | unwrap duration_seconds(duration)`

func histogramShardMapper(shards int, aggregations ...string) ShardMapper {
	return NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(shards)), nilShardMetrics, aggregations)
}

// histogramDownstream is the part of a query that a shard runs, for a mapped plan of two shards.
func histogramDownstream(expr string) string {
	return fmt.Sprintf("downstream<%s, shard=0_of_2> ++ downstream<%s, shard=1_of_2>", expr, expr)
}

// The shard of a histogram query returns the histogram of its part, and the frontend adds them up and reads the
// result once. A reader is not pushed down, because the quantile of two histograms is not the quantile of
// their sum.
func TestShardMapper_HistogramOverTime(t *testing.T) {
	const (
		over   = `histogram_over_time(` + histogramShardRange + `[5m])`
		custom = `histogram_over_time(buckets(1,2),` + histogramShardRange + `[5m])`
	)

	for name, tc := range map[string]struct {
		query string
		want  string
	}{
		"a sum by pushes the sum down": {
			query: `histogram_quantile(0.99, sum by (path) (` + over + `))`,
			want:  `histogram_quantile(0.99,sum by (path)(` + histogramDownstream(`sum by (path)(`+over+`)`) + `))`,
		},
		"a sum without a grouping": {
			query: `histogram_count(sum(` + over + `))`,
			want:  `histogram_count(sum(` + histogramDownstream(`sum(`+over+`)`) + `))`,
		},
		"a sum without": {
			query: `histogram_count(sum without (path) (` + over + `))`,
			want:  `histogram_count(sum without (path)(` + histogramDownstream(`sum without (path)(`+over+`)`) + `))`,
		},
		"a grouping of the range aggregation is merged with a sum": {
			query: `histogram_count(` + over + ` by (path))`,
			want:  `histogram_count(sum by (path)(` + histogramDownstream(over+` by (path)`) + `))`,
		},
		"without a grouping the series are concatenated": {
			query: `histogram_fraction(0, 1, ` + over + `)`,
			want:  `histogram_fraction(0,1,` + histogramDownstream(over) + `)`,
		},
		"a stage that drops labels is merged with a sum": {
			query: `histogram_count(histogram_over_time({app="api"} | json | drop foo | unwrap duration_seconds(duration)[5m]))`,
			want: `histogram_count(sum without ()(` +
				histogramDownstream(`histogram_over_time({app="api"} | json | drop foo | unwrap duration_seconds(duration)[5m])`) + `))`,
		},
		"custom buckets are sent to the shards": {
			query: `histogram_count(` + custom + `)`,
			want:  `histogram_count(` + histogramDownstream(custom) + `)`,
		},
		"a part that is not histograms is sharded as well": {
			query: `histogram_count(sum(` + over + `)) + sum(count_over_time({app="api"}[5m]))`,
			want: `(histogram_count(sum(` + histogramDownstream(`sum(`+over+`)`) + `)) + ` +
				`sum(` + histogramDownstream(`sum(count_over_time({app="api"}[5m]))`) + `))`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			noop, _, mapped, err := histogramShardMapper(2, ShardHistogramOverTime).Parse(syntax.MustParseExpr(tc.query))
			require.NoError(t, err)
			require.False(t, noop)
			require.Equal(t, tc.want, mapped.String())
		})
	}

	t.Run("a shard returns a histogram, so its query is one that a query cannot be", func(t *testing.T) {
		for query, want := range map[string]string{
			`sum by (path) (` + over + `)`: `sum by (path)(` + histogramDownstream(`sum by (path)(`+over+`)`) + `)`,
			over + ` by (path)`:            `sum by (path)(` + histogramDownstream(over+` by (path)`) + `)`,
		} {
			expr, err := syntax.ParseExprWithoutValidation(query)
			require.NoError(t, err)
			noop, _, mapped, err := histogramShardMapper(2, ShardHistogramOverTime).Parse(expr)
			require.NoError(t, err)
			require.False(t, noop)
			require.Equal(t, want, mapped.String())
		}
	})
}

// Sharding histograms is opt in, as sharding the other aggregations that are not additive is, and a query
// runs unsharded without it.
func TestShardMapper_HistogramOverTimeIsOptIn(t *testing.T) {
	const over = `histogram_over_time(` + histogramShardRange + `[5m])`

	for name, query := range map[string]string{
		"a quantile of a sum":       `histogram_quantile(0.99, sum by (path) (` + over + `))`,
		"a count of a sum":          `histogram_count(sum(` + over + `))`,
		"a reader of the range":     `histogram_count(` + over + ` by (path))`,
		"a reader without grouping": `histogram_fraction(0, 1, ` + over + `)`,
		"custom buckets":            `histogram_count(histogram_over_time(buckets(1, 2), ` + histogramShardRange + `[5m]))`,
	} {
		for aggregations, mapper := range map[string]ShardMapper{
			"none":                  histogramShardMapper(2),
			"the other aggregators": histogramShardMapper(2, ShardQuantileOverTime, ShardFirstOverTime, ShardLastOverTime, SupportApproxTopk),
		} {
			t.Run(name+", "+aggregations+" enabled", func(t *testing.T) {
				expr := syntax.MustParseExpr(query)
				noop, _, mapped, err := mapper.Parse(expr)
				require.NoError(t, err)
				require.True(t, noop)
				require.Equal(t, expr.String(), mapped.String())
			})
		}
	}

	// The histogram that a shard returns is not a query that the parser accepts, but the mapper is given plans
	// that were not parsed, and it must not shard what is not enabled whatever the root of the plan is.
	for name, query := range map[string]string{
		"a sum of the range":           `sum by (path) (` + over + `)`,
		"a sum without a grouping":     `sum(` + over + `)`,
		"the range with a grouping":    over + ` by (path)`,
		"the range without a grouping": over,
	} {
		t.Run("the histogram at the root of a plan, "+name, func(t *testing.T) {
			expr, err := syntax.ParseExprWithoutValidation(query)
			require.NoError(t, err)
			noop, _, mapped, err := histogramShardMapper(2).Parse(expr)
			require.NoError(t, err)
			require.True(t, noop)
			require.Equal(t, expr.String(), mapped.String())
		})
	}

	// The part of the query that is not histograms is sharded whether it is enabled or not.
	t.Run("the other part of a query is sharded", func(t *testing.T) {
		_, _, mapped, err := histogramShardMapper(2).Parse(syntax.MustParseExpr(
			`histogram_count(sum(` + over + `)) + sum(count_over_time({app="api"}[5m]))`))
		require.NoError(t, err)
		require.Contains(t, mapped.String(), histogramDownstream(`sum(count_over_time({app="api"}[5m]))`))
		require.Contains(t, mapped.String(), `histogram_count(sum(`+over+`))`, "the histogram part is not changed")
	})
}

// requireSameMatrix fails unless got has the series of want, with the same values. A tolerance of zero is an exact
// match: the buckets of a histogram are whole numbers, so what is read from them does not depend on how the series
// was sharded. The sum of the observations is a sum of floats, and it does depend on the order of the additions.
func requireSameMatrix(t *testing.T, want, got promql.Matrix, tolerance float64) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].Metric, got[i].Metric)
		require.Lenf(t, got[i].Floats, len(want[i].Floats), "series %s", want[i].Metric)
		for j, w := range want[i].Floats {
			g := got[i].Floats[j]
			require.Equal(t, w.T, g.T, "series %s", want[i].Metric)
			requireSameShardedFloat(t, w.F, g.F, tolerance, "series %s at %d", want[i].Metric, w.T)
		}
	}
}

func requireSameShardedFloat(t *testing.T, want, got, tolerance float64, msgAndArgs ...any) {
	t.Helper()
	switch {
	case math.IsNaN(want):
		require.True(t, math.IsNaN(got), msgAndArgs...)
	case math.IsInf(want, 0) || tolerance == 0 || want == 0:
		require.Equal(t, want, got, msgAndArgs...)
	default:
		require.InEpsilon(t, want, got, tolerance, msgAndArgs...)
	}
}

func requireSameVector(t *testing.T, want, got promql.Vector, tolerance float64) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].Metric, got[i].Metric)
		requireSameShardedFloat(t, want[i].F, got[i].F, tolerance, "series %s", want[i].Metric)
	}
}

// The result of a query is the same whether it is sharded or not. The streams are spread over the shards by a hash of
// their labels, and a series is in more than one shard when a stage drops the label that tells them apart.
func TestMappingEquivalenceHistograms(t *testing.T) {
	const (
		rounds  = 10
		streams = 600
		rng     = `{a=~".+"} | logfmt | unwrap value`
		window  = `[1s]`
	)

	// The bucket counts are exact, and so is everything read from them. The sum of the observations is not.
	for _, tc := range []struct {
		name      string
		query     string
		tolerance float64
	}{
		{"a quantile of the sum", `histogram_quantile(0.9, sum by (a) (histogram_over_time(` + rng + window + `)))`, 0},
		{"a quantile without a grouping", `histogram_quantile(0.5, sum(histogram_over_time(` + rng + window + `)))`, 0},
		{"several quantiles", `histogram_quantiles(sum by (a) (histogram_over_time(` + rng + window + `)), "quantile", 0.5, 0.9, 0.99)`, 0},
		{"a fraction", `histogram_fraction(10, 50, sum by (a) (histogram_over_time(` + rng + window + `)))`, 0},
		{"a count of the range aggregation", `histogram_count(histogram_over_time(` + rng + window + `) by (a))`, 0},
		{"a count that is not grouped", `histogram_count(histogram_over_time(` + rng + window + `))`, 0},
		{"a sum without", `histogram_count(sum without (b, c, d, index) (histogram_over_time(` + rng + window + `)))`, 0},
		{"an offset", `histogram_count(sum by (a) (histogram_over_time(` + rng + window + ` offset 2s)))`, 0},
		{"custom buckets", `histogram_fraction(5, 20, sum by (a) (histogram_over_time(buckets(5, 10, 20, 50), ` + rng + window + `)))`, 0},
		{"custom buckets and a quantile", `histogram_quantile(0.9, sum by (a) (histogram_over_time(buckets(5, 10, 20, 50), ` + rng + window + `)))`, 0},
		{"a stage that drops the label of the stream", `histogram_count(sum without () (histogram_over_time({a=~".+"} | logfmt | drop index | unwrap value ` + window + `)))`, 0},
		{"a drop under a grouping", `histogram_quantile(0.9, sum by (a, b) (histogram_over_time({a=~".+"} | logfmt | drop index | unwrap value ` + window + `)))`, 0},
		{"a stage that renames the label of the stream", `histogram_count(histogram_over_time({a=~".+"} | logfmt | label_format renamed=index | unwrap value ` + window + `))`, 0},
		{"the sum of the observations", `histogram_sum(sum by (a) (histogram_over_time(` + rng + window + `)))`, 1e-9},
		{"the average of the observations", `histogram_avg(sum by (a) (histogram_over_time(` + rng + window + `)))`, 1e-9},
	} {
		for _, shards := range []int{2, 3, 8} {
			t.Run(fmt.Sprintf("%s, %d shards", tc.name, shards), func(t *testing.T) {
				var (
					data    = randomStreams(streams, rounds+1, shards, []string{"a", "b", "c", "d"}, true)
					opts    = EngineOpts{EnableHistograms: true}
					regular = NewEngine(opts, NewMockQuerier(shards, data), NoLimits, log.NewNopLogger())
					sharded = NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())
					mapper  = histogramShardMapper(shards, ShardHistogramOverTime)
					ctx     = user.InjectOrgID(context.Background(), "fake")
				)

				run := func(t *testing.T, params Params) (want, got logqlmodel.Result) {
					t.Helper()
					noop, _, mapped, err := mapper.Parse(params.GetExpression())
					require.NoError(t, err)
					require.False(t, noop, "the query is sharded")
					require.Contains(t, mapped.String(), "downstream<")

					want, err = regular.Query(params).Exec(ctx)
					require.NoError(t, err)
					got, err = sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped}).Exec(ctx)
					require.NoError(t, err)
					return want, got
				}

				t.Run("range", func(t *testing.T) {
					params, err := NewLiteralParams(tc.query, time.Unix(1, 0), time.Unix(rounds, 0), time.Second, 0, logproto.FORWARD, 100, nil, nil)
					require.NoError(t, err)
					want, got := run(t, params)

					wantMatrix, ok := want.Data.(promql.Matrix)
					require.True(t, ok, "%T", want.Data)
					require.NotEmpty(t, wantMatrix, "the query has a result to compare")
					requireSameMatrix(t, wantMatrix, got.Data.(promql.Matrix), tc.tolerance)
				})

				t.Run("instant", func(t *testing.T) {
					params, err := NewLiteralParams(tc.query, time.Unix(5, 0), time.Unix(5, 0), 0, 0, logproto.FORWARD, 100, nil, nil)
					require.NoError(t, err)
					want, got := run(t, params)

					wantVector, ok := want.Data.(promql.Vector)
					require.True(t, ok, "%T", want.Data)
					require.NotEmpty(t, wantVector, "the query has a result to compare")
					requireSameVector(t, wantVector, got.Data.(promql.Vector), tc.tolerance)
				})
			})
		}
	}
}

// histogramCollidingStreams returns streams that are all one series once the label that tells them apart is dropped, so
// that every shard holds a part of the series, and the histogram of the series is the sum of the histograms of the shards.
// Each stream has a value for each second, so the window of a second of each step holds one value of each stream.
func histogramCollidingStreams(streams, seconds int) []logproto.Stream {
	r := rand.New(rand.NewSource(7)) //#nosec G404 -- Generation of test data only, no need for a cryptographic PRNG -- nosemgrep: math-random-used
	out := make([]logproto.Stream, 0, streams)
	for i := 0; i < streams; i++ {
		s := logproto.Stream{Labels: fmt.Sprintf(`{app="api", id="%d"}`, i)}
		for j := 0; j <= seconds; j++ {
			s.Entries = append(s.Entries, logproto.Entry{
				Timestamp: time.Unix(0, int64(j)*int64(time.Second)+1+r.Int63n(time.Second.Nanoseconds()-1)),
				Line:      fmt.Sprintf("value=%f", r.Float64()*100),
			})
		}
		out = append(out, s)
	}
	return out
}

// A series that is on more than one shard has to be added up by the frontend, which the mapper does with a sum when a
// stage can make the labels of two streams equal. Without it the frontend would return a sample for each shard.
func TestMappingEquivalenceHistograms_aSeriesOnManyShards(t *testing.T) {
	const (
		streams = 200
		seconds = 10
		rng     = `{app="api"} | logfmt | drop id | unwrap value`
	)

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"the range aggregation merges the shards", `histogram_count(histogram_over_time(` + rng + ` [1s]))`},
		{"a grouping of the range aggregation", `histogram_count(histogram_over_time(` + rng + ` [1s]) by (app))`},
		{"a quantile of the range aggregation", `histogram_quantile(0.9, histogram_over_time(` + rng + ` [1s]))`},
		{"a quantile of a sum", `histogram_quantile(0.9, sum(histogram_over_time(` + rng + ` [1s])))`},
		{"custom buckets", `histogram_fraction(10, 50, histogram_over_time(buckets(10, 50, 90), ` + rng + ` [1s]))`},
	} {
		for _, shards := range []int{2, 5} {
			t.Run(fmt.Sprintf("%s, %d shards", tc.name, shards), func(t *testing.T) {
				var (
					opts    = EngineOpts{EnableHistograms: true}
					regular = NewEngine(opts, NewMockQuerier(shards, histogramCollidingStreams(streams, seconds)), NoLimits, log.NewNopLogger())
					sharded = NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())
					ctx     = user.InjectOrgID(context.Background(), "fake")
				)

				params, err := NewLiteralParams(tc.query, time.Unix(1, 0), time.Unix(seconds, 0), time.Second, 0, logproto.FORWARD, 100, nil, nil)
				require.NoError(t, err)
				noop, _, mapped, err := histogramShardMapper(shards, ShardHistogramOverTime).Parse(params.GetExpression())
				require.NoError(t, err)
				require.False(t, noop)

				want, err := regular.Query(params).Exec(ctx)
				require.NoError(t, err)
				got, err := sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped}).Exec(ctx)
				require.NoError(t, err)

				wantMatrix := want.Data.(promql.Matrix)
				require.Len(t, wantMatrix, 1, "the streams are one series")
				require.Len(t, wantMatrix[0].Floats, seconds, "a point for each step")
				requireSameMatrix(t, wantMatrix, got.Data.(promql.Matrix), 0)
			})
		}
	}

	t.Run("the series has the values of all the shards", func(t *testing.T) {
		const shards = 3
		opts := EngineOpts{EnableHistograms: true}
		regular := NewEngine(opts, NewMockQuerier(shards, histogramCollidingStreams(streams, seconds)), NoLimits, log.NewNopLogger())
		sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())
		ctx := user.InjectOrgID(context.Background(), "fake")

		params, err := NewLiteralParams(`histogram_count(histogram_over_time(`+rng+` [1s]))`, time.Unix(1, 0), time.Unix(seconds, 0), time.Second, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)
		_, _, mapped, err := histogramShardMapper(shards, ShardHistogramOverTime).Parse(params.GetExpression())
		require.NoError(t, err)
		got, err := sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped}).Exec(ctx)
		require.NoError(t, err)

		matrix := got.Data.(promql.Matrix)
		require.Len(t, matrix, 1)
		for _, p := range matrix[0].Floats {
			require.Equal(t, float64(streams), p.F, "a value of each stream, at %d", p.T)
		}
	})
}

// histogramWireDownstreamer is the MockDownstreamer with the histograms of each shard sent as a querier sends them
// to the frontend, which reads them as it does a response it does not trust, before they are added up.
type histogramWireDownstreamer struct{ MockDownstreamer }

func (d histogramWireDownstreamer) Downstreamer(context.Context) Downstreamer { return d }

func (d histogramWireDownstreamer) Downstream(ctx context.Context, queries []DownstreamQuery, acc Accumulator) ([]logqlmodel.Result, error) {
	return d.MockDownstreamer.Downstream(ctx, queries, histogramWireAccumulator{acc})
}

type histogramWireAccumulator struct{ Accumulator }

func (a histogramWireAccumulator) Accumulate(ctx context.Context, res logqlmodel.Result, i int) error {
	if m, ok := res.Data.(HistogramMatrix); ok {
		p, err := m.ToProto()
		if err != nil {
			return err
		}
		b, err := p.Marshal()
		if err != nil {
			return err
		}
		var read logproto.HistogramMatrix
		if err := read.Unmarshal(b); err != nil {
			return err
		}
		if res.Data, err = HistogramMatrixFromProto(&read); err != nil {
			return err
		}
	}
	return a.Accumulator.Accumulate(ctx, res, i)
}

// histogramNonFiniteStreams returns streams that have a value for each second, of which the values of a second
// are the same kind for all the streams, and the kinds go round: finite values only, finite values and NaN, finite
// values and +Inf, finite values and -Inf, and finite values and both of the infinities.
func histogramNonFiniteStreams(streams, seconds int) []logproto.Stream {
	r := rand.New(rand.NewSource(11)) //#nosec G404 -- Generation of test data only, no need for a cryptographic PRNG -- nosemgrep: math-random-used
	out := make([]logproto.Stream, 0, streams)
	for i := 0; i < streams; i++ {
		s := logproto.Stream{Labels: fmt.Sprintf(`{app="api", id="%d"}`, i)}
		for j := 0; j <= seconds; j++ {
			value := fmt.Sprintf("%f", r.Float64()*100)
			// A tenth of the streams have the other kind of value, so that there are a few of them in a step.
			if r.Intn(10) == 0 {
				switch j % 5 {
				case 1:
					value = "NaN"
				case 2:
					value = "+Inf"
				case 3:
					value = "-Inf"
				case 4:
					value = []string{"+Inf", "-Inf"}[r.Intn(2)]
				}
			}
			s.Entries = append(s.Entries, logproto.Entry{
				Timestamp: time.Unix(0, int64(j)*int64(time.Second)+1+r.Int63n(time.Second.Nanoseconds()-1)),
				Line:      "value=" + value,
			})
		}
		out = append(out, s)
	}
	return out
}

// NaN and the infinities are values like the others, so the histograms of the shards have them, and the result of a
// query is the same whether it is sharded or not. The histograms of the shards go over the wire, where a NaN makes
// the count of a histogram more than its buckets hold. The sum of the observations is NaN or infinite for some of
// the steps, and a number for the others.
func TestMappingEquivalenceHistograms_nonFiniteValues(t *testing.T) {
	const (
		streams = 100
		seconds = 10
		rng     = `{app="api"} | logfmt | unwrap value`
	)

	for _, tc := range []struct {
		name      string
		query     string
		tolerance float64
	}{
		{"a count", `histogram_count(sum by (app) (histogram_over_time(` + rng + ` [1s])))`, 0},
		{"a count of the range aggregation", `histogram_count(histogram_over_time(` + rng + ` [1s]) by (app))`, 0},
		{"a quantile", `histogram_quantile(0.9, sum by (app) (histogram_over_time(` + rng + ` [1s])))`, 0},
		{"the median", `histogram_quantile(0.5, histogram_over_time(` + rng + ` [1s]) by (app))`, 0},
		{"a fraction", `histogram_fraction(10, 50, sum by (app) (histogram_over_time(` + rng + ` [1s])))`, 0},
		{"custom buckets", `histogram_quantile(0.9, sum by (app) (histogram_over_time(buckets(10, 50, 90), ` + rng + ` [1s])))`, 0},
		{"custom buckets and a fraction", `histogram_fraction(10, 50, sum by (app) (histogram_over_time(buckets(10, 50, 90), ` + rng + ` [1s])))`, 0},
		{"the sum of the observations", `histogram_sum(sum by (app) (histogram_over_time(` + rng + ` [1s])))`, 1e-9},
		{"the average of the observations", `histogram_avg(sum by (app) (histogram_over_time(` + rng + ` [1s])))`, 1e-9},
	} {
		for _, shards := range []int{2, 5} {
			t.Run(fmt.Sprintf("%s, %d shards", tc.name, shards), func(t *testing.T) {
				var (
					opts    = EngineOpts{EnableHistograms: true}
					regular = NewEngine(opts, NewMockQuerier(shards, histogramNonFiniteStreams(streams, seconds)), NoLimits, log.NewNopLogger())
					sharded = NewDownstreamEngine(opts, histogramWireDownstreamer{MockDownstreamer{regular}}, NoLimits, log.NewNopLogger())
					ctx     = user.InjectOrgID(context.Background(), "fake")
				)

				params, err := NewLiteralParams(tc.query, time.Unix(1, 0), time.Unix(seconds, 0), time.Second, 0, logproto.FORWARD, 100, nil, nil)
				require.NoError(t, err)
				noop, _, mapped, err := histogramShardMapper(shards, ShardHistogramOverTime).Parse(params.GetExpression())
				require.NoError(t, err)
				require.False(t, noop)

				want, err := regular.Query(params).Exec(ctx)
				require.NoError(t, err)
				got, err := sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped}).Exec(ctx)
				require.NoError(t, err)

				wantMatrix := want.Data.(promql.Matrix)
				require.Len(t, wantMatrix, 1)
				require.Len(t, wantMatrix[0].Floats, seconds, "a point for each step")
				requireSameMatrix(t, wantMatrix, got.Data.(promql.Matrix), tc.tolerance)

				// The steps are not all alike, or the query would not tell a sum that is a number from one that is not.
				if tc.name == "the sum of the observations" {
					var numbers, nans, posInfs, negInfs int
					for _, p := range wantMatrix[0].Floats {
						switch {
						case math.IsNaN(p.F):
							nans++
						case math.IsInf(p.F, 1):
							posInfs++
						case math.IsInf(p.F, -1):
							negInfs++
						default:
							numbers++
						}
					}
					require.Positive(t, numbers)
					require.Positive(t, nans)
					require.Positive(t, posInfs)
					require.Positive(t, negInfs)
				}
			})
		}
	}
}

// histogramStubDownstreamer gives the part of a sharded query that each shard runs a result of its own.
type histogramStubDownstreamer struct {
	results []logqlmodel.Result
}

func (d histogramStubDownstreamer) Downstreamer(context.Context) Downstreamer { return d }

func (d histogramStubDownstreamer) Downstream(ctx context.Context, queries []DownstreamQuery, acc Accumulator) ([]logqlmodel.Result, error) {
	if len(queries) != len(d.results) {
		return nil, fmt.Errorf("%d queries and %d results", len(queries), len(d.results))
	}
	for i := range queries {
		if err := acc.Accumulate(ctx, d.results[i], i); err != nil {
			return nil, err
		}
	}
	return acc.Result(), nil
}

// histogramOfBuckets returns a histogram with an observation in each of the buckets of the exponential schema from
// first up to but not including last.
func histogramOfBuckets(t *testing.T, first, last int) HistogramMatrix {
	t.Helper()
	var values []float64
	for i := first; i < last; i++ {
		values = append(values, histogramValueInBucket(i))
	}
	return HistogramMatrix{{histogramSample(60_000, "a", histogramObserved(t, nil, values...))}}
}

// A histogram of a shard is under the limit of buckets, and the sum of the histograms of all the shards can be over
// it. The query then fails, as it does when it is not sharded: the buckets of the sum are the union of the buckets
// of its parts, so a sum that is over the limit does not depend on how the series was divided.
func TestShardedHistograms_theLimitOfBucketsAppliesToTheSumOfTheShards(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	const query = `histogram_count(sum(histogram_over_time(` + histogramShardRange + `[1m])))`

	params, err := NewLiteralParams(query, histogramAt(60), histogramAt(60), 0, 0, logproto.FORWARD, 100, nil, nil)
	require.NoError(t, err)
	_, _, mapped, err := histogramShardMapper(2, ShardHistogramOverTime).Parse(params.GetExpression())
	require.NoError(t, err)

	run := func(shard0, shard1 HistogramMatrix) (logqlmodel.Result, error) {
		engine := NewDownstreamEngine(EngineOpts{EnableHistograms: true}, histogramStubDownstreamer{results: []logqlmodel.Result{
			{Data: shard0}, {Data: shard1},
		}}, NoLimits, log.NewNopLogger())
		return engine.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped}).Exec(ctx)
	}

	t.Run("the buckets of the shards add up to the limit", func(t *testing.T) {
		res, err := run(histogramOfBuckets(t, 0, 100), histogramOfBuckets(t, 60, 160))
		require.NoError(t, err)
		vec, ok := res.Data.(promql.Vector)
		require.True(t, ok, "%T", res.Data)
		require.Len(t, vec, 1)
		require.Equal(t, 200.0, vec[0].F, "the observations of both shards")
	})

	t.Run("the buckets of the shards add up to more than the limit", func(t *testing.T) {
		_, err := run(histogramOfBuckets(t, 0, 100), histogramOfBuckets(t, 100, 200))
		require.ErrorIs(t, err, logqlerr.ErrLimit)
		require.ErrorContains(t, err, "non-empty buckets")
	})
}
