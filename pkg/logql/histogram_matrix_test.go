package logql

import (
	"context"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func histogramSample(t int64, lbs string, h *histogram.FloatHistogram) promql.Sample {
	return promql.Sample{T: t, Metric: labels.FromStrings("app", lbs), H: h}
}

// requireSameHistogram fails unless got is want, including the hint that FloatHistogram.Equals does not look at.
func requireSameHistogram(t *testing.T, want, got *histogram.FloatHistogram) {
	t.Helper()
	require.True(t, want.Equals(got), "want %s, got %s", want, got)
	require.Equal(t, want.CounterResetHint, got.CounterResetHint)
}

// histogramThroughTheWire returns m after it is sent, as the bytes of the protobuf that a querier writes.
func histogramThroughTheWire(t *testing.T, m HistogramMatrix) HistogramMatrix {
	t.Helper()
	p, err := m.ToProto()
	require.NoError(t, err)
	b, err := p.Marshal()
	require.NoError(t, err)

	var read logproto.HistogramMatrix
	require.NoError(t, read.Unmarshal(b))
	out, err := HistogramMatrixFromProto(&read)
	require.NoError(t, err)
	return out
}

func TestHistogramMatrix_roundTrip(t *testing.T) {
	custom := &syntax.BucketSpec{Bounds: []float64{0.5, 1, 5, 10}}
	sum := histogramObserved(t, nil, 1, 2, 3).Copy()
	_, _, _, err := sum.KahanAdd(histogramObserved(t, nil, 2, 400, -3), nil)
	require.NoError(t, err)

	for name, m := range map[string]HistogramMatrix{
		"an empty matrix": {},
		"steps without series": {
			{}, {},
		},
		"exponential buckets, with negative values and zero": {
			{histogramSample(1000, "a", histogramObserved(t, nil, 1, 2, 2, 100, -5, 0, 1e-9, -0.25))},
		},
		"custom buckets": {
			{histogramSample(1000, "a", histogramObserved(t, custom, 0.1, 0.5, 0.6, 7, 70, 70))},
		},
		"an empty histogram": {
			{histogramSample(1000, "a", histogramObserved(t, nil)), histogramSample(1000, "b", histogramObserved(t, custom))},
		},
		"the sum of histograms": {
			{histogramSample(1000, "a", sum)},
		},
		"a step in between has no series": {
			{histogramSample(1000, "a", histogramObserved(t, nil, 1))},
			{},
			{histogramSample(3000, "a", histogramObserved(t, nil, 2)), histogramSample(3000, "b", histogramObserved(t, nil, 3))},
		},
		"many buckets": {
			{histogramSample(1000, "a", histogramObserved(t, nil, 1, 3, 9, 27, 81, 243, 729, 2187, 6561, 19683, 1e9, -1e9, 1e-3, 1e-6))},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := histogramThroughTheWire(t, m)
			require.Len(t, got, len(m))
			for i := range m {
				require.Len(t, got[i], len(m[i]), "step %d", i)
				for j, want := range m[i] {
					require.Equal(t, want.T, got[i][j].T)
					require.Equal(t, want.Metric, got[i][j].Metric)
					require.Zero(t, got[i][j].F)
					requireSameHistogram(t, want.H, got[i][j].H)
				}
			}
		})
	}
}

func TestHistogramMatrix_type(t *testing.T) {
	m := HistogramMatrix{}
	require.EqualValues(t, HistogramMatrixType, m.Type())
	require.NotEmpty(t, m.String())
}

// The counts are sent as integers, so a histogram that has one that is not is an error and not a rounding.
func TestHistogramMatrix_toProtoRefusesWhatItCannotHold(t *testing.T) {
	for name, h := range map[string]func(h *histogram.FloatHistogram){
		"a fraction in the count":      func(h *histogram.FloatHistogram) { h.Count = 3.5 },
		"a negative count":             func(h *histogram.FloatHistogram) { h.Count = -1 },
		"a count that is not a number": func(h *histogram.FloatHistogram) { h.Count = math.NaN() },
		"an infinite count":            func(h *histogram.FloatHistogram) { h.Count = math.Inf(1) },
		"a count over the integers":    func(h *histogram.FloatHistogram) { h.Count = 1 << 63 },
		"a fraction in the zero count": func(h *histogram.FloatHistogram) { h.ZeroCount = 0.5 },
		"a fraction in a bucket":       func(h *histogram.FloatHistogram) { h.PositiveBuckets[0] = 0.25 },
		"a negative bucket":            func(h *histogram.FloatHistogram) { h.NegativeBuckets[0] = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			hist := histogramObserved(t, nil, 1, 2, -3).Copy()
			h(hist)
			_, err := HistogramMatrix{{histogramSample(1000, "a", hist)}}.ToProto()
			require.Error(t, err)
		})
	}

	t.Run("a sample that has no histogram", func(t *testing.T) {
		_, err := HistogramMatrix{{promql.Sample{T: 1000, F: 1, Metric: labels.FromStrings("app", "a")}}}.ToProto()
		require.ErrorContains(t, err, "no histogram")
	})
}

// A response is not trusted: whatever it holds is summed by the frontend.
func TestHistogramMatrix_fromProtoRefusesWhatIsNotAHistogramOfAQuery(t *testing.T) {
	valid := func() *logproto.NativeHistogram {
		p, err := histogramToProto(histogramObserved(t, nil, 1, 2, 2, -3))
		require.NoError(t, err)
		return p
	}
	// manyBuckets has pos buckets on the positive side and neg on the negative one, which each hold one value.
	manyBuckets := func(pos, neg int) *logproto.NativeHistogram {
		side := func(n int) ([]*logproto.BucketSpan, []int64) {
			if n == 0 {
				return nil, nil
			}
			deltas := make([]int64, n)
			deltas[0] = 1 // the others hold one value as the one before them does
			return []*logproto.BucketSpan{{Offset: 0, Length: uint32(n)}}, deltas
		}
		p := &logproto.NativeHistogram{Schema: 3, ZeroThreshold: prometheus.DefNativeHistogramZeroThreshold, Count: uint64(pos + neg), Sum: 1}
		p.PositiveSpans, p.PositiveDeltas = side(pos)
		p.NegativeSpans, p.NegativeDeltas = side(neg)
		return p
	}
	// emptyBuckets has n buckets that hold no value on the positive side, or on the negative one.
	emptyBuckets := func(n int, negative bool) *logproto.NativeHistogram {
		spans, deltas := []*logproto.BucketSpan{{Offset: 0, Length: uint32(n)}}, make([]int64, n)
		if negative {
			return &logproto.NativeHistogram{Schema: 3, ZeroThreshold: prometheus.DefNativeHistogramZeroThreshold, NegativeSpans: spans, NegativeDeltas: deltas}
		}
		return &logproto.NativeHistogram{Schema: 3, ZeroThreshold: prometheus.DefNativeHistogramZeroThreshold, PositiveSpans: spans, PositiveDeltas: deltas}
	}
	// emptySpans has n spans that hold no bucket.
	emptySpans := func(n int) *logproto.NativeHistogram {
		spans := make([]*logproto.BucketSpan, n)
		for i := range spans {
			spans[i] = &logproto.BucketSpan{Offset: 1}
		}
		return &logproto.NativeHistogram{Schema: 3, ZeroThreshold: prometheus.DefNativeHistogramZeroThreshold, PositiveSpans: spans}
	}

	for name, tc := range map[string]struct {
		h       *logproto.NativeHistogram
		wantErr string
		wantIs  error
	}{
		"no histogram":              {h: nil, wantErr: "no histogram"},
		"a hint that is not known":  {h: func() *logproto.NativeHistogram { p := valid(); p.CounterResetHint = 9; return p }(), wantErr: "counter reset hint"},
		"spans that miss a bucket":  {h: func() *logproto.NativeHistogram { p := valid(); p.PositiveSpans[0].Length++; return p }(), wantErr: "bucket"},
		"buckets without spans":     {h: func() *logproto.NativeHistogram { p := valid(); p.PositiveSpans = nil; return p }(), wantErr: "bucket"},
		"a count that does not add": {h: func() *logproto.NativeHistogram { p := valid(); p.Count += 5; return p }(), wantErr: "count"},
		"a negative bucket":         {h: func() *logproto.NativeHistogram { p := valid(); p.PositiveDeltas[0] = -4; return p }(), wantErr: "negative"},
		"a schema that is not known": {
			h: func() *logproto.NativeHistogram { p := valid(); p.Schema = 42; return p }(), wantErr: "schema",
		},
		"too many custom bounds": {
			h: func() *logproto.NativeHistogram {
				bounds := make([]float64, syntax.MaxBucketBounds+1)
				for i := range bounds {
					bounds[i] = float64(i)
				}
				return &logproto.NativeHistogram{Schema: histogram.CustomBucketsSchema, CustomValues: bounds}
			}(),
			wantErr: "custom bounds",
		},
		"too many buckets on a side":                  {h: manyBuckets(maxHistogramBuckets+1, 0), wantErr: "positive side"},
		"too many non-empty buckets on the two sides": {h: manyBuckets(maxHistogramBuckets/2+1, maxHistogramBuckets/2), wantErr: "non-empty buckets", wantIs: logqlerr.ErrLimit},
		"as many as are allowed":                      {h: manyBuckets(maxHistogramBuckets, 0)},
		"as many on the two sides as are allowed":     {h: manyBuckets(maxHistogramBuckets/2, maxHistogramBuckets/2)},
		// Prometheus accepts these, and they have no non-empty bucket to count.
		"a run of empty buckets":                      {h: emptyBuckets(maxHistogramBuckets+1, false), wantErr: "positive side"},
		"a run of empty buckets on the negative side": {h: emptyBuckets(maxHistogramBuckets+1, true), wantErr: "negative side"},
		"many spans that hold no bucket":              {h: emptySpans(maxHistogramBuckets + 1), wantErr: "positive side"},
		"as many empty buckets as are allowed":        {h: emptyBuckets(maxHistogramBuckets, false)},
		"a span that holds no bucket":                 {h: emptySpans(1), wantErr: "holds no bucket"},
		"as many spans that hold none as are allowed": {h: emptySpans(maxHistogramBuckets), wantErr: "holds no bucket"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := HistogramMatrixFromProto(&logproto.HistogramMatrix{Values: []*logproto.HistogramVector{{
				Samples: []*logproto.HistogramSample{{H: tc.h, TimestampMs: 1000}},
			}}})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
			}
		})
	}
}

// The buckets that a response holds are in memory when they are read, so what the reader may do is not to make
// more of them: ToFloat makes a copy of the same size, and the sum of the frontend walks through them. A histogram
// that is too large is refused before that.
func TestHistogramMatrix_fromProtoDoesNotCopyWhatItRefuses(t *testing.T) {
	const n = 1 << 22 // 32 MiB as floats
	p := &logproto.NativeHistogram{
		Schema:         3,
		PositiveSpans:  []*logproto.BucketSpan{{Offset: 0, Length: n}},
		PositiveDeltas: make([]int64, n),
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := histogramFromProto(p)
	runtime.ReadMemStats(&after)

	require.ErrorContains(t, err, "positive side")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "the histogram was copied before it was refused")
}

func TestHistogramMatrixStepEvaluator(t *testing.T) {
	a, b := histogramObserved(t, nil, 1), histogramObserved(t, nil, 2)
	m := HistogramMatrix{
		{histogramSample(60_000, "a", a)},
		{},
		{histogramSample(180_000, "a", a), histogramSample(180_000, "b", b)},
	}

	t.Run("a range query has a vector for each step", func(t *testing.T) {
		params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, histogramAt(60), histogramAt(180), time.Minute, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)

		ev, err := NewResultStepEvaluator(logqlmodel.Result{Data: m}, params)
		require.NoError(t, err)

		var got []int
		for _, wantTs := range []int64{60_000, 120_000, 180_000} {
			ok, ts, r := ev.Next()
			require.True(t, ok)
			require.Equal(t, wantTs, ts)
			got = append(got, len(r.SampleVector()))
		}
		require.Equal(t, []int{1, 0, 2}, got)
		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.NoError(t, ev.Error())
		require.NoError(t, ev.Close())
	})

	t.Run("an instant query has one", func(t *testing.T) {
		params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, histogramAt(60), histogramAt(60), 0, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)

		ev := NewHistogramMatrixStepEvaluator(m[:1], params)
		ok, ts, r := ev.Next()
		require.True(t, ok)
		require.EqualValues(t, 60_000, ts)
		require.Len(t, r.SampleVector(), 1)
		require.Same(t, a, r.SampleVector()[0].H)
		ok, _, _ = ev.Next()
		require.False(t, ok)
	})

	// The shards of a query are read together, and what they return is one vector for each step.
	t.Run("the matrices of the shards are concatenated step by step", func(t *testing.T) {
		params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, histogramAt(60), histogramAt(120), time.Minute, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)

		first := HistogramMatrix{{histogramSample(60_000, "a", a)}, {histogramSample(120_000, "a", a)}}
		second := HistogramMatrix{{histogramSample(60_000, "b", b)}, {}}
		var evs []StepEvaluator
		for _, m := range []HistogramMatrix{first, second} {
			ev, err := NewResultStepEvaluator(logqlmodel.Result{Data: m}, params)
			require.NoError(t, err)
			evs = append(evs, ev)
		}

		ev := NewConcatStepEvaluator(evs)
		var got []int
		for ok, _, r := ev.Next(); ok; ok, _, r = ev.Next() {
			got = append(got, len(r.SampleVector()))
		}
		require.Equal(t, []int{2, 1}, got)
	})
}

// histogramShardEngine returns an engine over a querier with two series that have values in the window of each
// of the two steps of the queries of the tests.
func histogramShardEngine(maxSeries int) *QueryEngine {
	return NewEngine(EngineOpts{EnableHistograms: true}, NewMockQuerier(0, histogramShardStreams()), &fakeLimits{maxSeries: maxSeries, timeout: time.Minute}, log.NewNopLogger())
}

func histogramShardStreams() []logproto.Stream {
	return []logproto.Stream{
		histogramStream(`{app="a"}`,
			histogramTimedValue{histogramSeconds(10), "0.1"}, histogramTimedValue{histogramSeconds(20), "0.2"},
			histogramTimedValue{histogramSeconds(70), "2.0"}),
		histogramStream(`{app="b"}`,
			histogramTimedValue{histogramSeconds(15), "5"},
			histogramTimedValue{histogramSeconds(80), "6"}, histogramTimedValue{histogramSeconds(90), "7"}),
	}
}

// histogramShardQuery runs a query that the validation of a query would refuse, as a shard of a query does, because
// its result is a histogram. The shard is the only one, so that it reads every stream.
func histogramShardQuery(t *testing.T, eng *QueryEngine, query string, start, end time.Time, step time.Duration) (logqlmodel.Result, error) {
	t.Helper()
	return histogramPlanQuery(t, eng, query, []string{"0_of_1"}, start, end, step)
}

// histogramPlanQuery runs the plan of a query that is not parsed, with the shards that the query has.
func histogramPlanQuery(t *testing.T, eng *QueryEngine, query string, shards []string, start, end time.Time, step time.Duration) (logqlmodel.Result, error) {
	t.Helper()
	expr, err := syntax.ParseExprWithoutValidation(query)
	require.NoError(t, err)
	params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, start, end, step, 0, logproto.FORWARD, 100, shards, nil)
	require.NoError(t, err)
	return eng.Query(ParamsWithExpressionOverride{Params: params, ExpressionOverride: expr}).Exec(user.InjectOrgID(context.Background(), "fake"))
}

// A plan is not parsed, so the parser is not what keeps a histogram result from a user. The engine runs a query
// that returns one only when it has shards, which is what a shard of a query has and a query of a user does not.
func TestEngine_AHistogramAtTheRootNeedsShards(t *testing.T) {
	const rng = `{app=~".+"} | logfmt | unwrap duration`

	for name, query := range map[string]string{
		"histogram_over_time": `histogram_over_time(` + rng + ` [1m])`,
		"a sum by":            `sum by (app) (histogram_over_time(` + rng + ` [1m]))`,
		"a grouped range":     `histogram_over_time(` + rng + ` [1m]) by (app)`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, shards := range [][]string{nil, {}} {
				res, err := histogramPlanQuery(t, histogramShardEngine(100), query, shards, histogramAt(60), histogramAt(120), time.Minute)
				require.ErrorIs(t, err, logqlerr.ErrParse, "%v", res.Data)
				require.ErrorContains(t, err, "a query cannot return a histogram")
			}

			res, err := histogramPlanQuery(t, histogramShardEngine(100), query, []string{"0_of_1"}, histogramAt(60), histogramAt(120), time.Minute)
			require.NoError(t, err)
			require.IsType(t, HistogramMatrix{}, res.Data)
		})
	}

	// A query that reads the histogram has floats at its root, and it needs no shard.
	t.Run("a reader needs none", func(t *testing.T) {
		res, err := histogramPlanQuery(t, histogramShardEngine(100), `histogram_count(sum by (app) (histogram_over_time(`+rng+` [1m])))`, nil, histogramAt(60), histogramAt(120), time.Minute)
		require.NoError(t, err)
		require.IsType(t, promql.Matrix{}, res.Data)
	})

	// It is refused before anything is read.
	t.Run("nothing is read", func(t *testing.T) {
		var read atomic.Int64
		q := newCountingQuerier(NewMockQuerier(0, histogramShardStreams()), &read)
		eng := NewEngine(EngineOpts{EnableHistograms: true}, q, &fakeLimits{maxSeries: 100, timeout: time.Minute}, log.NewNopLogger())
		const query = `sum by (app) (histogram_over_time(` + rng + ` [1m]))`

		_, err := histogramPlanQuery(t, eng, query, nil, histogramAt(60), histogramAt(120), time.Minute)
		require.ErrorIs(t, err, logqlerr.ErrParse)
		require.Zero(t, read.Load())

		// The same query is read when it is a shard, so the count is one that the query has.
		_, err = histogramPlanQuery(t, eng, query, []string{"0_of_1"}, histogramAt(60), histogramAt(120), time.Minute)
		require.NoError(t, err)
		require.NotZero(t, read.Load())
	})
}

func TestEngine_AHistogramAtTheRootIsAHistogramMatrix(t *testing.T) {
	const rng = `{app=~".+"} | logfmt | unwrap duration`

	for name, query := range map[string]string{
		"histogram_over_time": `histogram_over_time(` + rng + ` [1m])`,
		"a sum by":            `sum by (app) (histogram_over_time(` + rng + ` [1m]))`,
		"a sum without":       `sum without (foo) (histogram_over_time(` + rng + ` [1m]))`,
		"a grouped range":     `histogram_over_time(` + rng + ` [1m]) by (app)`,
	} {
		t.Run(name+"/range", func(t *testing.T) {
			res, err := histogramShardQuery(t, histogramShardEngine(100), query, histogramAt(60), histogramAt(120), time.Minute)
			require.NoError(t, err)

			m, ok := res.Data.(HistogramMatrix)
			require.True(t, ok, "%T", res.Data)
			require.Len(t, m, 2)

			counts := func(vec SampleVector) map[string]float64 {
				out := map[string]float64{}
				for _, s := range vec {
					out[s.Metric.Get("app")] += s.H.Count
				}
				return out
			}
			// Each step has its own vector. The range iterator reuses the one it builds, so a vector that was not
			// copied would show the last step in both.
			require.Equal(t, map[string]float64{"a": 2, "b": 1}, counts(m[0]), "the lines of 0s to 60s")
			require.Equal(t, map[string]float64{"a": 1, "b": 2}, counts(m[1]), "the lines of 60s to 120s")
			for step, wantTs := range []int64{60_000, 120_000} {
				for _, s := range m[step] {
					require.Equal(t, wantTs, s.T)
					require.NoError(t, s.H.Validate())
				}
			}
		})

		t.Run(name+"/instant", func(t *testing.T) {
			res, err := histogramShardQuery(t, histogramShardEngine(100), query, histogramAt(120), histogramAt(120), 0)
			require.NoError(t, err)
			m, ok := res.Data.(HistogramMatrix)
			require.True(t, ok, "%T", res.Data)
			require.Len(t, m, 1)
			require.Len(t, m[0], 2)
		})
	}

	t.Run("a step without series is kept", func(t *testing.T) {
		res, err := histogramShardQuery(t, histogramShardEngine(100), `sum by (app) (histogram_over_time(`+rng+` [1m]))`, histogramAt(300), histogramAt(420), time.Minute)
		require.NoError(t, err)
		m, ok := res.Data.(HistogramMatrix)
		require.True(t, ok, "%T", res.Data)
		require.Len(t, m, 3)
		for _, vec := range m {
			require.Empty(t, vec)
		}
	})

	t.Run("the series limit applies to the series of all the steps", func(t *testing.T) {
		_, err := histogramShardQuery(t, histogramShardEngine(1), `sum by (app) (histogram_over_time(`+rng+` [1m]))`, histogramAt(60), histogramAt(120), time.Minute)
		require.ErrorIs(t, err, logqlerr.ErrLimit)

		// One series that is in both steps is one series.
		_, err = histogramShardQuery(t, histogramShardEngine(1), `sum by (app) (histogram_over_time({app="a"} | logfmt | unwrap duration [1m]))`, histogramAt(60), histogramAt(120), time.Minute)
		require.NoError(t, err)
	})

	t.Run("the flag still applies", func(t *testing.T) {
		eng := NewEngine(EngineOpts{}, NewMockQuerier(0, nil), NoLimits, log.NewNopLogger())
		_, err := histogramShardQuery(t, eng, `sum by (app) (histogram_over_time(`+rng+` [1m]))`, histogramAt(60), histogramAt(60), 0)
		require.ErrorIs(t, err, logqlerr.ErrParse)
		require.ErrorContains(t, err, "-"+histogramFlagName)
	})
}
