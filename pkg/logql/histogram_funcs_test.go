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
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
)

// histogramRunQuery runs a query like the engine of a querier does, with the histograms enabled: the query is parsed
// and validated, and its result is the one that the API returns.
func histogramRunQuery(t *testing.T, streams []logproto.Stream, query string, start, end time.Time, step time.Duration) (promql_parser.Value, error) {
	t.Helper()
	return histogramRunQueryAt(t, EngineOpts{EnableHistograms: true}, streams, query, start, end, step)
}

// histogramRunQueryWith runs an instant query at 60s with the given options, and returns its vector.
func histogramRunQueryWith(t *testing.T, opts EngineOpts, streams []logproto.Stream, query string) (promql.Vector, error) {
	t.Helper()
	res, err := histogramRunQueryAt(t, opts, streams, query, histogramAt(60), histogramAt(60), 0)
	if err != nil {
		return nil, err
	}
	vec, ok := res.(promql.Vector)
	require.True(t, ok, "%T", res)
	return vec, nil
}

func histogramRunQueryAt(t *testing.T, opts EngineOpts, streams []logproto.Stream, query string, start, end time.Time, step time.Duration) (promql_parser.Value, error) {
	t.Helper()
	eng := NewEngine(opts, NewMockQuerier(0, streams), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(query, start, end, step, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)
	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// histogramInstant returns the result of an histogramInstant query at 60s, as a value for each series.
func histogramInstant(t *testing.T, streams []logproto.Stream, query string) map[string]float64 {
	t.Helper()
	res, err := histogramRunQuery(t, streams, query, histogramAt(60), histogramAt(60), 0)
	require.NoError(t, err, query)
	vec, ok := res.(promql.Vector)
	require.True(t, ok, "%s returned a %T", query, res)
	out := make(map[string]float64, len(vec))
	for _, s := range vec {
		require.Nil(t, s.H, "the result of a query is a float")
		out[s.Metric.String()] = s.F
	}
	return out
}

// histogramScalar returns the value of the only series of an instant query.
func histogramScalar(t *testing.T, streams []logproto.Stream, query string) float64 {
	t.Helper()
	got := histogramInstant(t, streams, query)
	require.Len(t, got, 1, query)
	for _, v := range got {
		return v
	}
	return math.NaN()
}

// histogramSevenValues are the values of a stream. In the buckets 1, 2, 4 and 8 they are:
//
//	(-Inf, 1]: 0.5
//	(1, 2]:    1.5, 1.5
//	(2, 4]:    3
//	(4, 8]:    6, 6
//	(8, +Inf): 100
var histogramSevenValues = []logproto.Stream{
	histogramStream(`{app="a"}`,
		histogramTimedValue{histogramSeconds(10), "0.5"}, histogramTimedValue{histogramSeconds(15), "1.5"}, histogramTimedValue{histogramSeconds(20), "1.5"},
		histogramTimedValue{histogramSeconds(25), "3"}, histogramTimedValue{histogramSeconds(30), "6"}, histogramTimedValue{histogramSeconds(35), "6"}, histogramTimedValue{histogramSeconds(40), "100"}),
}

const histogramSevenBuckets = `histogram_over_time(buckets(1, 2, 4, 8), {app="a"} | logfmt | unwrap duration [1m])`

func TestHistogramFuncs_countSumAvg(t *testing.T) {
	require.Equal(t, 7.0, histogramScalar(t, histogramSevenValues, `histogram_count(`+histogramSevenBuckets+`)`))
	require.InDelta(t, 118.5, histogramScalar(t, histogramSevenValues, `histogram_sum(`+histogramSevenBuckets+`)`), 1e-9)
	require.InDelta(t, 118.5/7, histogramScalar(t, histogramSevenValues, `histogram_avg(`+histogramSevenBuckets+`)`), 1e-9)

	// The buckets do not change what is counted, they only place it.
	const exponential = `histogram_over_time({app="a"} | logfmt | unwrap duration [1m])`
	require.Equal(t, 7.0, histogramScalar(t, histogramSevenValues, `histogram_count(`+exponential+`)`))
	require.InDelta(t, 118.5, histogramScalar(t, histogramSevenValues, `histogram_sum(`+exponential+`)`), 1e-9)
}

// The expected values are worked out from the definition of the quantile of a histogram: the
// observations of a bucket are taken to be evenly spread in it, and the buckets above the last bound
// return that bound.
func TestHistogramFuncs_quantile(t *testing.T) {
	for _, tc := range []struct {
		q    float64
		want float64
		why  string
	}{
		{0.1, 0.7, "rank 0.7 of the 1 in (0, 1]"},
		{0.5, 3.0, "rank 3.5, which is the middle of the 1 in (2, 4]"},
		{0.6, 4.4, "rank 4.2, which is a tenth into the 2 in (4, 8]"},
		{0.9, 8, "rank 6.3 is in the bucket above the last bound, which returns that bound"},
		{1, 8, "the highest value is in the bucket above the last bound"},
	} {
		t.Run(fmt.Sprint(tc.q), func(t *testing.T) {
			got := histogramScalar(t, histogramSevenValues, fmt.Sprintf(`histogram_quantile(%v, %s)`, tc.q, histogramSevenBuckets))
			require.InDelta(t, tc.want, got, 1e-9, tc.why)
		})
	}

	t.Run("a quantile out of range", func(t *testing.T) {
		require.True(t, math.IsInf(histogramScalar(t, histogramSevenValues, `histogram_quantile(-0.5, `+histogramSevenBuckets+`)`), -1))
		require.True(t, math.IsInf(histogramScalar(t, histogramSevenValues, `histogram_quantile(1.5, `+histogramSevenBuckets+`)`), 1))
	})
}

func TestHistogramFuncs_fraction(t *testing.T) {
	for _, tc := range []struct {
		lower, upper string
		want         float64
		why          string
	}{
		{"0", "2", 3.0 / 7, "0.5, 1.5 and 1.5, with the bound 2 on the edge of a bucket"},
		{"2", "8", 3.0 / 7, "3, 6 and 6"},
		{"1", "8", 5.0 / 7, "1.5, 1.5, 3, 6 and 6"},
		{"-Inf", "+Inf", 1, "everything, the bucket above the last bound included"},
		{"-Inf", "1", 1.0 / 7, "less than or equal to the first bound"},
		{"4", "4", 0, "an empty interval"},
		{"8", "2", 0, "an interval that ends before it starts"},
	} {
		t.Run(tc.lower+"_"+tc.upper, func(t *testing.T) {
			got := histogramScalar(t, histogramSevenValues, fmt.Sprintf(`histogram_fraction(%s, %s, %s)`, tc.lower, tc.upper, histogramSevenBuckets))
			require.InDelta(t, tc.want, got, 1e-9, tc.why)
		})
	}

	t.Run("a threshold on a bound is exact", func(t *testing.T) {
		// "The share of requests that are 2 or less", which the bounds were chosen to answer.
		got := histogramScalar(t, histogramSevenValues, `histogram_fraction(0, 2, `+histogramSevenBuckets+`)`)
		require.Equal(t, 3.0/7, got)
	})
}

func TestHistogramFuncs_quantiles(t *testing.T) {
	got := histogramInstant(t, histogramSevenValues, `histogram_quantiles(`+histogramSevenBuckets+`, "quantile", 0.1, 0.5, 0.9, 0.999)`)
	require.Len(t, got, 4)
	require.InDelta(t, 0.7, got[`{app="a", quantile="0.1"}`], 1e-9)
	require.InDelta(t, 3.0, got[`{app="a", quantile="0.5"}`], 1e-9)
	require.InDelta(t, 8.0, got[`{app="a", quantile="0.9"}`], 1e-9)
	require.InDelta(t, 8.0, got[`{app="a", quantile="0.999"}`], 1e-9)

	t.Run("the label has the name that was asked for", func(t *testing.T) {
		got := histogramInstant(t, histogramSevenValues, `histogram_quantiles(`+histogramSevenBuckets+`, "q", 0.5)`)
		require.Equal(t, map[string]float64{`{app="a", q="0.5"}`: 3}, got)
	})

	t.Run("the same values as histogram_quantile", func(t *testing.T) {
		for _, q := range []string{"0.1", "0.5", "0.9"} {
			single := histogramScalar(t, histogramSevenValues, `histogram_quantile(`+q+`, `+histogramSevenBuckets+`)`)
			many := histogramInstant(t, histogramSevenValues, `histogram_quantiles(`+histogramSevenBuckets+`, "quantile", `+q+`, 0.99)`)
			require.Equal(t, single, many[`{app="a", quantile="`+q+`"}`])
		}
	})
}

func TestHistogramFuncs_noObservations(t *testing.T) {
	// Every line is a number that is not finite, so the series has lines but no observation.
	streams := []logproto.Stream{histogramStream(`{app="n"}`, histogramTimedValue{histogramSeconds(10), "NaN"}, histogramTimedValue{histogramSeconds(20), "+Inf"})}
	const h = `histogram_over_time({app="n"} | logfmt | unwrap duration [1m])`

	require.Equal(t, 0.0, histogramScalar(t, streams, `histogram_count(`+h+`)`))
	require.Equal(t, 0.0, histogramScalar(t, streams, `histogram_sum(`+h+`)`))
	require.True(t, math.IsNaN(histogramScalar(t, streams, `histogram_avg(`+h+`)`)))
	require.True(t, math.IsNaN(histogramScalar(t, streams, `histogram_quantile(0.5, `+h+`)`)))
	require.True(t, math.IsNaN(histogramScalar(t, streams, `histogram_fraction(0, 1, `+h+`)`)))

	t.Run("a window without a line has no series", func(t *testing.T) {
		require.Empty(t, histogramInstant(t, histogramSevenValues, `histogram_count(histogram_over_time({app="a"} | logfmt | unwrap duration [1m] offset 10m))`))
	})
}

// With many observations, a histogram with the default buckets gives the quantiles of the values closely.
//
// A bucket of schema 3 is 9% wider than the one before it, so a value is known to within 4.4% of the middle
// of its bucket, and a quantile is interpolated inside its bucket. How close the interpolation is depends on
// the observations in the bucket. Measured against quantile_over_time on log-normal values, the mean error
// is 0.1% for 100000 observations and the worst one is 1.3%. For 5000 it is 0.3% and 5%. For 600 the worst
// is 25%, and for 100 it is 79%. That is the tail: the exact quantile of a few observations is an
// interpolation between values that are far apart, and the histogram does not have them.
func TestHistogramFuncs_quantileMatchesQuantileOverTime(t *testing.T) {
	const n = 20000
	r := rand.New(rand.NewSource(1))
	entries := make([]histogramTimedValue, 0, n)
	for i := 0; i < n; i++ {
		// A log-normal distribution, like a latency: from a millisecond to ten seconds.
		v := math.Exp(r.NormFloat64()*1.2 - 1)
		entries = append(entries, histogramTimedValue{time.Duration(i+1) * 2900 * time.Microsecond, strconv.FormatFloat(v, 'g', -1, 64)})
	}
	streams := []logproto.Stream{histogramStream(`{app="a"}`, entries...)}
	const rng = `{app="a"} | logfmt | unwrap duration [1m]`

	// A quantile that is one bucket off is 9% off, so this catches it.
	const tolerance = 0.05
	for _, q := range []string{"0.1", "0.5", "0.9", "0.99"} {
		t.Run(q, func(t *testing.T) {
			exact := histogramScalar(t, streams, `quantile_over_time(`+q+`, `+rng+`)`)
			estimated := histogramScalar(t, streams, `histogram_quantile(`+q+`, histogram_over_time(`+rng+`))`)
			require.InEpsilon(t, exact, estimated, tolerance, "exact %v, estimated %v", exact, estimated)
		})
	}
}

func TestHistogramFuncs_withTheRestOfTheQuery(t *testing.T) {
	streams := []logproto.Stream{
		histogramStream(`{app="a", pod="1"}`, histogramTimedValue{histogramSeconds(10), "0.5"}, histogramTimedValue{histogramSeconds(20), "1.5"}),
		histogramStream(`{app="a", pod="2"}`, histogramTimedValue{histogramSeconds(15), "3"}, histogramTimedValue{histogramSeconds(25), "6"}),
		histogramStream(`{app="b", pod="1"}`, histogramTimedValue{histogramSeconds(12), "2"}),
	}
	const rng = `{app=~".+"} | logfmt | unwrap duration [1m]`

	t.Run("on the sum of the series of an app", func(t *testing.T) {
		got := histogramInstant(t, streams, `histogram_count(sum by (app) (histogram_over_time(`+rng+`)))`)
		require.Equal(t, map[string]float64{`{app="a"}`: 4, `{app="b"}`: 1}, got)
	})

	t.Run("the grouping can be in the range aggregation", func(t *testing.T) {
		inside := histogramInstant(t, streams, `histogram_sum(histogram_over_time(`+rng+`) by (app))`)
		outside := histogramInstant(t, streams, `histogram_sum(sum by (app) (histogram_over_time(`+rng+`)))`)
		require.Equal(t, inside, outside)
		require.InDelta(t, 11, inside[`{app="a"}`], 1e-9)
	})

	t.Run("without grouping it is a series for each stream", func(t *testing.T) {
		got := histogramInstant(t, streams, `histogram_count(histogram_over_time(`+rng+`))`)
		require.Len(t, got, 3)
	})

	t.Run("the result is a float, so it goes in the arithmetic of a query", func(t *testing.T) {
		got := histogramInstant(t, streams, `histogram_sum(sum by (app) (histogram_over_time(`+rng+`))) / histogram_count(sum by (app) (histogram_over_time(`+rng+`)))`)
		require.InDelta(t, 11.0/4, got[`{app="a"}`], 1e-9)
		require.InDelta(t, 2, got[`{app="b"}`], 1e-9)

		got = histogramInstant(t, streams, `sum(histogram_count(sum by (app) (histogram_over_time(`+rng+`))))`)
		require.Equal(t, map[string]float64{`{}`: 5}, got)

		got = histogramInstant(t, streams, `histogram_quantile(0.5, sum by (app) (histogram_over_time(`+rng+`))) > 1`)
		require.Contains(t, got, `{app="a"}`)
	})

	t.Run("label_replace on the result", func(t *testing.T) {
		got := histogramInstant(t, streams, `label_replace(histogram_count(sum by (app) (histogram_over_time(`+rng+`))), "application", "$1", "app", "(.*)")`)
		require.Equal(t, 4.0, got[`{app="a", application="a"}`])
	})

	t.Run("a range query has a value at each step", func(t *testing.T) {
		res, err := histogramRunQuery(t, streams, `histogram_count(sum by (app) (histogram_over_time({app=~".+"} | logfmt | unwrap duration [30s])))`, histogramAt(30), histogramAt(60), 15*time.Second)
		require.NoError(t, err)
		matrix, ok := res.(promql.Matrix)
		require.True(t, ok, "%T", res)
		require.Len(t, matrix, 2)

		var seriesA promql.Series
		for _, s := range matrix {
			if s.Metric.Get("app") == "a" {
				seriesA = s
			}
		}
		// The windows of 30s that end at 30s, 45s and 60s hold the lines at (0, 30], (15, 45] and (30, 60].
		require.Equal(t, []promql.FPoint{{T: 30_000, F: 4}, {T: 45_000, F: 2}}, seriesA.Floats[:2])
		for _, s := range matrix {
			require.Empty(t, s.Histograms)
		}
	})
}

func TestHistogramFuncs_errors(t *testing.T) {
	t.Run("a bucket limit error fails the query", func(t *testing.T) {
		values := make([]histogramTimedValue, 0, maxHistogramBuckets+1)
		for i := 0; i <= maxHistogramBuckets; i++ {
			values = append(values, histogramTimedValue{histogramSeconds(1 + i%50), strconv.FormatFloat(histogramValueInBucket(i), 'g', -1, 64)})
		}
		streams := []logproto.Stream{histogramStream(`{app="a"}`, values...)}

		_, err := histogramRunQuery(t, streams, `histogram_count(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrLimit)

		// The bounds are the way out, as the error says.
		_, err = histogramRunQuery(t, streams, `histogram_count(histogram_over_time(buckets(1, 10, 100), {app="a"} | logfmt | unwrap duration [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
	})

	t.Run("a reader of something that is not a histogram is a client error", func(t *testing.T) {
		// The parser does not allow it, so the query is not validated, as a caller that skips validation would do.
		streams := []logproto.Stream{histogramStream(`{app="a"}`, histogramTimedValue{histogramSeconds(10), "1"})}
		_, err := histogramEvaluate(t, streams, `histogram_count(rate({app="a"}[1m]))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrParse)
		require.ErrorContains(t, err, "histogram_count needs a histogram")
	})

	t.Run("a line that is not a number fails the query", func(t *testing.T) {
		streams := []logproto.Stream{histogramStream(`{app="a"}`, histogramTimedValue{histogramSeconds(10), "oops"})}
		_, err := histogramRunQuery(t, streams, `histogram_count(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrPipeline)

		_, err = histogramRunQuery(t, streams, `histogram_count(histogram_over_time({app="a"} | logfmt | unwrap duration | __error__="" [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.NoError(t, err)
	})

	t.Run("a series limit applies to the series that the query returns", func(t *testing.T) {
		streams := make([]logproto.Stream, 0, 5)
		for i := 0; i < 5; i++ {
			streams = append(streams, histogramStream(fmt.Sprintf(`{app="a", pod="%d"}`, i), histogramTimedValue{histogramSeconds(10), "1"}))
		}
		// Five histograms are one series each. The readers return a series for each histogram.
		got := histogramInstant(t, streams, `histogram_count(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`)
		require.Len(t, got, 5)
		got = histogramInstant(t, streams, `histogram_quantiles(histogram_over_time({app="a"} | logfmt | unwrap duration [1m]), "q", 0.5, 0.9)`)
		require.Len(t, got, 10, "a series for each quantile")
	})
}

// The label of the quantile replaces a label of the series that has the same name.
func TestHistogramFuncs_quantilesReplaceALabelOfTheSeries(t *testing.T) {
	streams := []logproto.Stream{histogramStream(`{app="a", quantile="old"}`, histogramTimedValue{histogramSeconds(10), "1"}, histogramTimedValue{histogramSeconds(20), "3"})}
	const h = `histogram_over_time(buckets(1, 2, 4), {app="a"} | logfmt | unwrap duration [1m])`

	want := histogramScalar(t, streams, `histogram_quantile(0.5, `+h+`)`)
	got := histogramInstant(t, streams, `histogram_quantiles(`+h+`, "quantile", 0.5)`)
	require.Equal(t, map[string]float64{`{app="a", quantile="0.5"}`: want}, got)
}
