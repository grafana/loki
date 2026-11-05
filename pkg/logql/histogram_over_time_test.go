package logql

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// histogramTimedValue is a value that a stream logs at a time since the start of the epoch.
type histogramTimedValue struct {
	at    time.Duration
	value string
}

// histogramStream returns a stream with one line, duration=<value>, for each value.
func histogramStream(lbs string, values ...histogramTimedValue) logproto.Stream {
	s := logproto.Stream{Labels: lbs}
	for _, v := range values {
		s.Entries = append(s.Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0).Add(v.at),
			Line:      "duration=" + v.value,
		})
	}
	return s
}

// histogramEvaluatedStep is the vector of a step.
type histogramEvaluatedStep struct {
	ts  int64
	vec promql.Vector
}

// histogramEvaluate runs query with the evaluator of a querier, step by step, and returns every step.
//
// The query is not validated. A histogram at the root of a query, which a query cannot return, is
// what the evaluator has to handle for the tests of its parts.
func histogramEvaluate(t *testing.T, streams []logproto.Stream, query string, start, end time.Time, step time.Duration) ([]histogramEvaluatedStep, error) {
	t.Helper()
	return histogramEvaluateWithCutoff(t, streams, query, start, end, step, 0)
}

// histogramEvaluateWithCutoff is evaluate with the early cut-off on max_query_series. Zero turns it off.
func histogramEvaluateWithCutoff(t *testing.T, streams []logproto.Stream, query string, start, end time.Time, step time.Duration, maxSeries int) ([]histogramEvaluatedStep, error) {
	t.Helper()

	expr, err := syntax.ParseExprWithoutValidation(query)
	require.NoError(t, err)
	sampleExpr, ok := expr.(syntax.SampleExpr)
	require.True(t, ok)

	// The query of the params is only a placeholder, the expression of the test is the override.
	params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, start, end, step, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)

	ev := NewDefaultEvaluator(NewMockQuerier(0, streams), 5*time.Minute, 0, NoLimits)
	ctx := user.InjectOrgID(context.Background(), "fake")
	se, err := ev.NewStepEvaluator(ctx, ev, sampleExpr, ParamsWithExpressionOverride{Params: params, ExpressionOverride: sampleExpr}, RootContext(sampleExpr, maxSeries))
	if err != nil {
		return nil, err
	}
	defer func() { require.NoError(t, se.Close()) }()

	var steps []histogramEvaluatedStep
	for {
		ok, ts, r := se.Next()
		if err := se.Error(); err != nil {
			return nil, err
		}
		if !ok {
			return steps, nil
		}
		// The evaluators reuse the vector of a step, so it is copied.
		steps = append(steps, histogramEvaluatedStep{ts: ts, vec: append(promql.Vector(nil), r.SampleVector()...)})
	}
}

func histogramMustEvaluate(t *testing.T, streams []logproto.Stream, query string, start, end time.Time, step time.Duration) []histogramEvaluatedStep {
	t.Helper()
	steps, err := histogramEvaluate(t, streams, query, start, end, step)
	require.NoError(t, err)
	return steps
}

// histogramByLabels indexes a vector by the labels of its samples.
func histogramByLabels(vec promql.Vector) map[string]promql.Sample {
	out := make(map[string]promql.Sample, len(vec))
	for _, s := range vec {
		out[s.Metric.String()] = s
	}
	return out
}

// histogramSeconds is a duration of n seconds.
func histogramSeconds(n int) time.Duration { return time.Duration(n) * time.Second }

// histogramAt is a point in time, in seconds from the start of the epoch.
func histogramAt(sec int) time.Time { return time.Unix(int64(sec), 0) }

func TestHistogramOverTime(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a"}`,
			histogramTimedValue{histogramSeconds(10), "0.1"}, histogramTimedValue{histogramSeconds(20), "0.2"},
			histogramTimedValue{histogramSeconds(70), "2.0"}, histogramTimedValue{histogramSeconds(80), "0.5"}, histogramTimedValue{histogramSeconds(90), "0.5"}),
	}
	const rng = `{app="a"} | logfmt | unwrap duration`

	t.Run("one histogram of the window for each step", func(t *testing.T) {
		steps := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+` [1m])`, histogramAt(60), histogramAt(120), time.Minute)
		require.Len(t, steps, 2)

		// The window of a step is above its start and up to its end.
		require.EqualValues(t, 60_000, steps[0].ts)
		require.Len(t, steps[0].vec, 1)
		require.Equal(t, `{app="a"}`, steps[0].vec[0].Metric.String())
		require.Equal(t, 2.0, steps[0].vec[0].H.Count, "the lines at 10s and 20s")
		require.InDelta(t, 0.3, steps[0].vec[0].H.Sum, 1e-9)

		require.EqualValues(t, 120_000, steps[1].ts)
		require.Equal(t, 3.0, steps[1].vec[0].H.Count, "the lines at 70s, 80s and 90s")
		require.InDelta(t, 3.0, steps[1].vec[0].H.Sum, 1e-9)
		for _, step := range steps {
			require.EqualValues(t, step.ts, step.vec[0].T)
			require.Equal(t, 0.0, step.vec[0].F, "the value is in H, and not in F")
			require.NoError(t, step.vec[0].H.Validate())
			require.Equal(t, histogram.GaugeType, step.vec[0].H.CounterResetHint)
		}
	})

	t.Run("the count is the number of lines, the sum is sum_over_time", func(t *testing.T) {
		hist := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+` [2m])`, histogramAt(60), histogramAt(180), time.Minute)
		sums := histogramMustEvaluate(t, streams, `sum_over_time(`+rng+` [2m])`, histogramAt(60), histogramAt(180), time.Minute)
		// Without an unwrap, every value of duration is a series of its own, so the lines are summed.
		counts := histogramMustEvaluate(t, streams, `sum(count_over_time({app="a"} | logfmt | duration > 0 [2m]))`, histogramAt(60), histogramAt(180), time.Minute)

		require.Len(t, hist, 3)
		require.Len(t, sums, 3)
		require.Len(t, counts, 3)
		for i := range hist {
			require.Equal(t, hist[i].ts, sums[i].ts)
			require.Len(t, hist[i].vec, 1)
			require.InDelta(t, sums[i].vec[0].F, hist[i].vec[0].H.Sum, 1e-9, "step %d", i)
			require.Equal(t, counts[i].vec[0].F, hist[i].vec[0].H.Count, "step %d", i)
		}
		// The windows overlap, so a line is in more than one of them.
		require.Equal(t, 2.0, hist[0].vec[0].H.Count)
		require.Equal(t, 5.0, hist[1].vec[0].H.Count)
		require.Equal(t, 3.0, hist[2].vec[0].H.Count)
	})

	t.Run("an offset shifts the window and not the timestamp", func(t *testing.T) {
		steps := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+` [1m] offset 1m)`, histogramAt(120), histogramAt(120), 0)
		require.Len(t, steps, 1)
		require.EqualValues(t, 120_000, steps[0].ts)
		require.Equal(t, 2.0, steps[0].vec[0].H.Count, "the window of 0s to 60s")
	})

	t.Run("an instant query has one step", func(t *testing.T) {
		steps := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+` [2m])`, histogramAt(120), histogramAt(120), 0)
		require.Len(t, steps, 1)
		require.Equal(t, 5.0, steps[0].vec[0].H.Count)
	})

	t.Run("a series with no line in the window has no histogram", func(t *testing.T) {
		steps := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+` [1m])`, histogramAt(300), histogramAt(300), 0)
		require.Len(t, steps, 1)
		require.Empty(t, steps[0].vec)
	})
}

func TestHistogramOverTime_customBuckets(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a"}`,
			histogramTimedValue{histogramSeconds(10), "0.1"}, histogramTimedValue{histogramSeconds(20), "0.15"}, histogramTimedValue{histogramSeconds(30), "0.2"}, histogramTimedValue{histogramSeconds(40), "7"}),
	}
	steps := histogramMustEvaluate(t, streams, `histogram_over_time(buckets(0.15, 1), {app="a"} | logfmt | unwrap duration [1m])`, histogramAt(60), histogramAt(60), 0)
	require.Len(t, steps, 1)

	h := steps[0].vec[0].H
	require.True(t, h.UsesCustomBuckets())
	require.Equal(t, []float64{0.15, 1}, h.CustomValues)
	require.Equal(t, []histogramBucketOf{
		{lower: math.Inf(-1), upper: 0.15, count: 2}, // 0.1 and 0.15, which is on the bound
		{lower: 0.15, upper: 1, count: 1},            // 0.2
		{lower: 1, upper: math.Inf(1), count: 1},     // 7
	}, histogramNonEmptyBucketsOf(h))
}

// A sum of histograms equals the histogram of the range aggregation grouped the same way: merging the
// histograms of the series is the same as building one histogram of all their values.
func TestHistogramOverTime_sum(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a", pod="1"}`, histogramTimedValue{histogramSeconds(10), "0.1"}, histogramTimedValue{histogramSeconds(20), "0.3"}, histogramTimedValue{histogramSeconds(30), "5"}),
		histogramStream(`{app="a", pod="2"}`, histogramTimedValue{histogramSeconds(15), "0.1"}, histogramTimedValue{histogramSeconds(25), "0.7"}, histogramTimedValue{histogramSeconds(35), "40"}),
		histogramStream(`{app="b", pod="1"}`, histogramTimedValue{histogramSeconds(12), "2"}),
	}
	const rng = `{app=~".+"} | logfmt | unwrap duration [1m]`

	equalHistograms := func(t *testing.T, want, got promql.Vector) {
		t.Helper()
		wantBy, gotBy := histogramByLabels(want), histogramByLabels(got)
		require.Len(t, gotBy, len(wantBy))
		for lbs, w := range wantBy {
			g, ok := gotBy[lbs]
			require.True(t, ok, "series %s", lbs)
			require.Equal(t, w.H.Count, g.H.Count, lbs)
			require.InDelta(t, w.H.Sum, g.H.Sum, 1e-9, lbs)
			require.Equal(t, histogramNonEmptyBucketsOf(w.H), histogramNonEmptyBucketsOf(g.H), lbs)
			require.NoError(t, g.H.Validate())
		}
	}

	grouped := histogramMustEvaluate(t, streams, `histogram_over_time(`+rng+`) by (app)`, histogramAt(60), histogramAt(60), 0)[0].vec
	require.Len(t, grouped, 2)

	t.Run("sum by is the same as by in the range aggregation", func(t *testing.T) {
		got := histogramMustEvaluate(t, streams, `sum by (app) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)[0].vec
		equalHistograms(t, grouped, got)
	})

	t.Run("sum without is the same as by in the range aggregation", func(t *testing.T) {
		got := histogramMustEvaluate(t, streams, `sum without (pod) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)[0].vec
		equalHistograms(t, grouped, got)
	})

	t.Run("a sum of sums", func(t *testing.T) {
		all := histogramMustEvaluate(t, streams, `sum(histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)[0].vec
		nested := histogramMustEvaluate(t, streams, `sum(sum by (app) (histogram_over_time(`+rng+`)))`, histogramAt(60), histogramAt(60), 0)[0].vec
		require.Len(t, all, 1)
		require.Empty(t, all[0].Metric)
		require.Equal(t, 7.0, all[0].H.Count)
		equalHistograms(t, all, nested)
	})

	t.Run("a sum is not the histogram of one of the series", func(t *testing.T) {
		got := histogramByLabels(histogramMustEvaluate(t, streams, `sum by (app) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)[0].vec)
		require.Equal(t, 6.0, got[`{app="a"}`].H.Count)
		require.InDelta(t, 46.2, got[`{app="a"}`].H.Sum, 1e-9)
		require.Equal(t, 1.0, got[`{app="b"}`].H.Count)
	})

	t.Run("the sum has the buckets of the values it merges", func(t *testing.T) {
		got := histogramByLabels(histogramMustEvaluate(t, streams, `sum by (app) (histogram_over_time(buckets(0.2, 1, 10), `+rng+`))`, histogramAt(60), histogramAt(60), 0)[0].vec)
		// app a: 0.1, 0.1 | 0.3, 0.7 | 5 | 40
		require.Equal(t, []histogramBucketOf{
			{lower: math.Inf(-1), upper: 0.2, count: 2},
			{lower: 0.2, upper: 1, count: 2},
			{lower: 1, upper: 10, count: 1},
			{lower: 10, upper: math.Inf(1), count: 1},
		}, histogramNonEmptyBucketsOf(got[`{app="a"}`].H))
	})

	t.Run("it does not change the histograms it adds", func(t *testing.T) {
		a := &histogram.FloatHistogram{Schema: 3, Count: 1, Sum: 1, ZeroThreshold: 1e-9, PositiveSpans: []histogram.Span{{Offset: 0, Length: 1}}, PositiveBuckets: []float64{1}}
		b := a.Copy()
		g := &groupedAggregation{}
		require.NoError(t, g.addToSum(promql.Sample{H: a}))
		require.NoError(t, g.addToSum(promql.Sample{H: b}))
		sum, err := g.histogramSum()
		require.NoError(t, err)
		require.Equal(t, 2.0, sum.Count)
		require.Equal(t, 1.0, a.Count, "the first histogram is copied before it is added to")
		require.Equal(t, 1.0, b.Count)
	})
}

func TestHistogramOverTime_pipelineErrors(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a"}`,
			histogramTimedValue{histogramSeconds(10), "0.1"}, histogramTimedValue{histogramSeconds(20), "oops"}, histogramTimedValue{histogramSeconds(30), "0.3"}),
	}

	t.Run("a line that is not a number fails the query, as it does for the other range aggregations", func(t *testing.T) {
		for _, query := range []string{
			`histogram_over_time({app="a"} | logfmt | unwrap duration [1m])`,
			`sum(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`,
			`sum(quantile_over_time(0.5, {app="a"} | logfmt | unwrap duration [1m]))`,
		} {
			_, err := histogramEvaluate(t, streams, query, histogramAt(60), histogramAt(60), 0)
			require.ErrorIs(t, err, logqlerr.ErrPipeline, query)
		}
	})

	t.Run("the error is not counted as a value in a histogram", func(t *testing.T) {
		for _, query := range []string{
			`histogram_over_time({app="a"} | logfmt | unwrap duration | __error__="" [1m])`,
			`sum(histogram_over_time({app="a"} | logfmt | unwrap duration | __error__="" [1m]))`,
		} {
			steps, err := histogramEvaluate(t, streams, query, histogramAt(60), histogramAt(60), 0)
			require.NoError(t, err, query)
			require.Len(t, steps[0].vec, 1)
			h := steps[0].vec[0].H
			require.Equal(t, 2.0, h.Count, "only the lines that are a number, %s", query)
			require.InDelta(t, 0.4, h.Sum, 1e-9)
			require.Zero(t, h.ZeroCount, "the line that is not a number is not a zero")
		}
	})

	t.Run("a query that keeps the errors gets them as a series of their own", func(t *testing.T) {
		steps, err := histogramEvaluate(t, streams, `histogram_over_time({app="a"} | logfmt | unwrap duration | __error__!="" [1m])`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
		require.Len(t, steps[0].vec, 1)
		require.True(t, steps[0].vec[0].Metric.Has(logqlerr.ErrorLabel))
		require.Equal(t, 1.0, steps[0].vec[0].H.Count)
	})
}

func TestHistogramOverTime_bucketLimit(t *testing.T) {
	// n lines, each in a bucket of its own, from the bucket first on.
	manyBuckets := func(lbs string, first, n int) logproto.Stream {
		values := make([]histogramTimedValue, 0, n)
		for i := 0; i < n; i++ {
			values = append(values, histogramTimedValue{histogramSeconds(1 + i%50), strconv.FormatFloat(histogramValueInBucket(first+i), 'g', -1, 64)})
		}
		return histogramStream(lbs, values...)
	}
	const rng = `{app="a"} | logfmt | unwrap duration [1m]`

	t.Run("a histogram over the limit fails the query", func(t *testing.T) {
		_, err := histogramEvaluate(t, []logproto.Stream{manyBuckets(`{app="a"}`, 0, maxHistogramBuckets+1)}, `histogram_over_time(`+rng+`)`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrLimit)

		steps, err := histogramEvaluate(t, []logproto.Stream{manyBuckets(`{app="a"}`, 0, maxHistogramBuckets)}, `histogram_over_time(`+rng+`)`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
		require.Len(t, histogramNonEmptyBucketsOf(steps[0].vec[0].H), maxHistogramBuckets)
	})

	t.Run("a sum over the limit fails, with every part under it", func(t *testing.T) {
		streams := []logproto.Stream{
			manyBuckets(`{app="a", pod="1"}`, 0, 100),
			manyBuckets(`{app="a", pod="2"}`, 100, 100),
		}
		// Each pod is a histogram of 100 buckets, and the app is one of 200.
		steps, err := histogramEvaluate(t, streams, `sum by (pod) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
		require.Len(t, steps[0].vec, 2)

		_, err = histogramEvaluate(t, streams, `sum by (app) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrLimit)
		_, err = histogramEvaluate(t, streams, `sum(histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrLimit)
		// The same series are grouped inside the range aggregation, and fail there.
		_, err = histogramEvaluate(t, streams, `histogram_over_time(`+rng+`) by (app)`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrLimit)
	})

	t.Run("overlapping parts under the limit add to a sum under it", func(t *testing.T) {
		streams := []logproto.Stream{
			manyBuckets(`{app="a", pod="1"}`, 0, 100),
			manyBuckets(`{app="a", pod="2"}`, 50, 100),
		}
		steps, err := histogramEvaluate(t, streams, `sum by (app) (histogram_over_time(`+rng+`))`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
		require.Len(t, histogramNonEmptyBucketsOf(steps[0].vec[0].H), 150)
		require.Equal(t, 200.0, steps[0].vec[0].H.Count)
	})
}

// The early cut-off on max_query_series applies to the iterator of histograms as it does to the others: a
// query that holds more series than the limit fails as soon as the iterator stores the one that is over.
func TestHistogramOverTime_seriesCutoff(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a", pod="1"}`, histogramTimedValue{histogramSeconds(10), "1"}),
		histogramStream(`{app="a", pod="2"}`, histogramTimedValue{histogramSeconds(10), "1"}),
		histogramStream(`{app="a", pod="3"}`, histogramTimedValue{histogramSeconds(10), "1"}),
	}
	// Without a grouping the root is a series for each stream, and its output cannot be smaller.
	const query = `histogram_over_time({app="a"} | logfmt | unwrap duration [1m])`

	_, err := histogramEvaluateWithCutoff(t, streams, query, histogramAt(60), histogramAt(60), 0, 2)
	require.ErrorIs(t, err, logqlerr.ErrLimit, "three series with a limit of two")

	steps, err := histogramEvaluateWithCutoff(t, streams, query, histogramAt(60), histogramAt(60), 0, 3)
	require.NoError(t, err, "three series with a limit of three")
	require.Len(t, steps[0].vec, 3)

	steps, err = histogramEvaluateWithCutoff(t, streams, query, histogramAt(60), histogramAt(60), 0, 0)
	require.NoError(t, err, "the cut-off is off")
	require.Len(t, steps[0].vec, 3)
}

func TestGroupedAggregation_addToSum(t *testing.T) {
	h := &histogram.FloatHistogram{Count: 1, Sum: 1}

	t.Run("floats", func(t *testing.T) {
		g := &groupedAggregation{}
		require.NoError(t, g.addToSum(promql.Sample{F: 1}))
		require.NoError(t, g.addToSum(promql.Sample{F: 2}))
		require.Equal(t, 3.0, g.sum.result())
		require.Nil(t, g.histogram)
	})

	t.Run("it is a client error", func(t *testing.T) {
		require.ErrorIs(t, errMixedHistogramsAndFloats, logqlerr.ErrParse)
	})

	t.Run("a histogram after a float", func(t *testing.T) {
		g := &groupedAggregation{}
		require.NoError(t, g.addToSum(promql.Sample{F: 1}))
		require.ErrorIs(t, g.addToSum(promql.Sample{H: h}), errMixedHistogramsAndFloats)
	})

	t.Run("a float after a histogram", func(t *testing.T) {
		g := &groupedAggregation{}
		require.NoError(t, g.addToSum(promql.Sample{H: h}))
		require.ErrorIs(t, g.addToSum(promql.Sample{F: 1}), errMixedHistogramsAndFloats)
		// A float that is zero is a float, and not the absence of a histogram.
		require.ErrorIs(t, g.addToSum(promql.Sample{F: 0}), errMixedHistogramsAndFloats)
	})
}

func TestHistogramOverTime_onlySumAddsHistograms(t *testing.T) {
	streams := []logproto.Stream{histogramStream(`{app="a"}`, histogramTimedValue{histogramSeconds(10), "0.1"})}
	for _, op := range []string{"avg", "max", "min", "count", "stddev", "stdvar"} {
		_, err := histogramEvaluate(t, streams, op+`(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorContains(t, err, op+" does not support histograms", op)
		// The shape of the query is what is wrong, so it is a client error that the frontend does not retry.
		require.ErrorIs(t, err, logqlerr.ErrParse, op)
	}
}

// A histogram is not a result. The parser does not allow a query that returns one, and the engine
// checks it again, so that one that gets through is an error and not a series of zeros.
func TestEngine_HistogramIsNotAResult(t *testing.T) {
	streams := []logproto.Stream{histogramStream(`{app="a"}`, histogramTimedValue{histogramSeconds(10), "0.1"})}
	eng := NewEngine(EngineOpts{EnableHistograms: true}, NewMockQuerier(0, streams), NoLimits, log.NewNopLogger())
	ctx := user.InjectOrgID(context.Background(), "fake")

	for name, rangeQuery := range map[string]bool{"range": true, "instant": false} {
		t.Run(name, func(t *testing.T) {
			expr, err := syntax.ParseExprWithoutValidation(`sum by (app) (histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`)
			require.NoError(t, err)

			var params Params
			if rangeQuery {
				params, err = NewLiteralParams(`count_over_time({app="a"}[1m])`, histogramAt(60), histogramAt(120), time.Minute, 0, logproto.FORWARD, 100, nil, nil)
			} else {
				params, err = NewLiteralParams(`count_over_time({app="a"}[1m])`, histogramAt(60), histogramAt(60), 0, 0, logproto.FORWARD, 100, nil, nil)
			}
			require.NoError(t, err)

			_, err = eng.Query(ParamsWithExpressionOverride{Params: params, ExpressionOverride: expr}).Exec(ctx)
			require.ErrorContains(t, err, "cannot return a histogram")
			require.ErrorIs(t, err, logqlerr.ErrParse)
		})
	}
}

// histogramTestIterator returns the iterator of histogram_over_time, with a step and a window of a minute, over
// the samples of a series. The steps are at the minutes from the first to the last.
func histogramTestIterator(t *testing.T, samples []logproto.Sample, first, last int) RangeVectorIterator {
	t.Helper()
	lbs, err := syntax.ParseLabels(`{app="a"}`)
	require.NoError(t, err)
	series := iter.NewSeriesIterator(logproto.Series{Labels: lbs.String(), Samples: samples, StreamHash: labels.StableHash(lbs)})
	it := iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator([]iter.SampleIterator{series}))
	minute := time.Minute.Nanoseconds()
	return newHistogramIterator(it, nil, minute, minute, int64(first)*minute, int64(last)*minute, 0, iteratorLimits{})
}

// A step that fails is not returned as a step without series: Next is false, and Error says why.
func TestHistogramRangeVectorIterator_failingStep(t *testing.T) {
	minute := time.Minute.Nanoseconds()
	samples := []logproto.Sample{{Timestamp: minute / 2, Value: 1}}
	// The window of the second step has one value in each of more buckets than a histogram may have.
	for i := 0; i <= maxHistogramBuckets; i++ {
		samples = append(samples, logproto.Sample{Timestamp: minute + int64(i+1), Value: histogramValueInBucket(i)})
	}
	// The window of the third step has one value.
	samples = append(samples, logproto.Sample{Timestamp: 2*minute + minute/2, Value: 1})

	it := histogramTestIterator(t, samples, 1, 3)
	require.True(t, it.Next())
	_, r := it.At()
	require.Len(t, r.SampleVector(), 1)
	require.NoError(t, it.Error())

	require.False(t, it.Next(), "the step that goes over the bucket limit is not returned")
	require.ErrorIs(t, it.Error(), logqlerr.ErrLimit)
	require.False(t, it.Next(), "a failed iterator stays failed")
	require.ErrorIs(t, it.Error(), logqlerr.ErrLimit)
}

// The vector of a step is reused by the next one, as it is for every range aggregation, but the histograms in it
// are not: the one that a caller kept from a step is the same after the next step.
func TestHistogramRangeVectorIterator_histogramsAreNotReused(t *testing.T) {
	minute := time.Minute.Nanoseconds()
	samples := []logproto.Sample{
		{Timestamp: minute / 2, Value: 1},
		{Timestamp: minute + minute/2, Value: 2},
		{Timestamp: minute + minute/2 + 1, Value: 3},
	}
	it := histogramTestIterator(t, samples, 1, 2)

	require.True(t, it.Next())
	_, r := it.At()
	first := r.SampleVector()[0].H
	require.Equal(t, 1.0, first.Count)

	require.True(t, it.Next())
	_, r = it.At()
	second := r.SampleVector()[0].H
	require.Equal(t, 2.0, second.Count)
	require.NotSame(t, first, second)
	require.Equal(t, 1.0, first.Count, "the histogram of the first step is not changed by the second")
	require.Equal(t, 1.0, first.Sum)
}

// BenchmarkHistogramRangeVectorIterator compares histogram_over_time with the range aggregations that also
// read every value of a window: sum_over_time, quantile_over_time and its sketch. Each of them goes through
// 5 steps of 10 series, with the number of values of the name of the benchmark in each window. The values
// are log-normal, so they are spread over a few dozen buckets.
func BenchmarkHistogramRangeVectorIterator(b *testing.B) {
	const (
		numSeries = 10
		numSteps  = 5
		window    = time.Minute
	)
	var (
		selRange = window.Nanoseconds()
		start    = window.Nanoseconds()
		end      = numSteps * window.Nanoseconds()
		median   = 0.5
	)

	variants := []struct {
		name    string
		new     func(it iter.PeekingSampleIterator) (RangeVectorIterator, error)
		release func(StepResult)
	}{
		{
			name: "sum_over_time",
			new: func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
				expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeSum}
				return newTimestampFirstRangeVectorIterator(it, expr, selRange, selRange, start, end, 0, iteratorLimits{})
			},
		},
		{
			name: "quantile_over_time",
			new: func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
				expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeQuantile, Params: &median}
				return newTimestampFirstRangeVectorIterator(it, expr, selRange, selRange, start, end, 0, iteratorLimits{})
			},
		},
		{
			name: "quantile_over_time_sketch",
			new: func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
				return newQuantileSketchIterator(it, selRange, selRange, start, end, 0, iteratorLimits{}), nil
			},
			release: func(r StepResult) { r.QuantileSketchVec().Release() },
		},
		{
			name: "histogram_over_time",
			new: func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
				return newHistogramIterator(it, nil, selRange, selRange, start, end, 0, iteratorLimits{}), nil
			},
		},
	}

	for _, values := range []int{10, 100, 1000} {
		rnd := rand.New(rand.NewSource(42))
		series := make([]logproto.Series, 0, numSeries)
		for s := 0; s < numSeries; s++ {
			lbs, err := syntax.ParseLabels(fmt.Sprintf(`{app="a", pod="%d"}`, s))
			require.NoError(b, err)
			samples := make([]logproto.Sample, 0, numSteps*values)
			for w := 0; w < numSteps; w++ {
				for i := 0; i < values; i++ {
					at := time.Duration(w)*window + window*time.Duration(i+1)/time.Duration(values+1)
					samples = append(samples, logproto.Sample{Timestamp: at.Nanoseconds(), Value: math.Exp(rnd.NormFloat64())})
				}
			}
			series = append(series, logproto.Series{Labels: lbs.String(), Samples: samples, StreamHash: labels.StableHash(lbs)})
		}
		input := func() iter.PeekingSampleIterator {
			its := make([]iter.SampleIterator, 0, len(series))
			for _, s := range series {
				its = append(its, iter.NewSeriesIterator(s))
			}
			return iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator(its))
		}

		for _, v := range variants {
			b.Run(fmt.Sprintf("%d-values/%s", values, v.name), func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					it, err := v.new(input())
					require.NoError(b, err)
					steps := 0
					for it.Next() {
						_, r := it.At()
						if v.release != nil {
							v.release(r)
						}
						steps++
					}
					require.NoError(b, it.Error())
					require.Equal(b, numSteps, steps)
					require.NoError(b, it.Close())
				}
			})
		}
	}
}
