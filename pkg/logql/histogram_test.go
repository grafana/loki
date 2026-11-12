package logql

import (
	"math"
	"math/rand"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/stretchr/testify/require"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// histogramBucketOf is a non-empty bucket of a histogram.
type histogramBucketOf struct {
	lower, upper float64
	count        float64
}

// histogramNonEmptyBucketsOf returns the non-empty buckets of h in order, from the lowest to the highest.
func histogramNonEmptyBucketsOf(h *histogram.FloatHistogram) []histogramBucketOf {
	var out []histogramBucketOf
	for it := h.AllBucketIterator(); it.Next(); {
		if b := it.At(); b.Count > 0 {
			out = append(out, histogramBucketOf{lower: b.Lower, upper: b.Upper, count: b.Count})
		}
	}
	return out
}

// histogramValueInBucket returns a value in the middle of the bucket i of schema 3, which is above 2^((i-1)/8) and
// up to 2^(i/8). A value on a bound is on a rounded number, and may be in the bucket next to it.
func histogramValueInBucket(i int) float64 {
	return math.Exp2((float64(i) - 0.5) / 8)
}

func histogramObserved(t *testing.T, spec *syntax.BucketSpec, values ...float64) *histogram.FloatHistogram {
	t.Helper()
	b := newHistogramBuilder(spec)
	for _, v := range values {
		b.observe(v)
	}
	h, err := b.build()
	require.NoError(t, err)
	require.NoError(t, h.Validate())
	return h
}

func TestHistogramBuilder_exponential(t *testing.T) {
	t.Run("the schema and the zero threshold", func(t *testing.T) {
		h := histogramObserved(t, nil, 1)
		require.EqualValues(t, 3, h.Schema)
		require.Equal(t, prometheus.DefNativeHistogramZeroThreshold, h.ZeroThreshold)
		require.Equal(t, histogram.GaugeType, h.CounterResetHint, "a window is not a counter")
		require.False(t, h.UsesCustomBuckets())
	})

	t.Run("the upper bound of a bucket belongs to it", func(t *testing.T) {
		// 1, 2, 4 and 0.5 are on a bucket bound, whatever the schema.
		for _, tc := range []struct {
			v            float64
			lower, upper float64
		}{
			{v: 1, lower: math.Exp2(-1.0 / 8), upper: 1},
			{v: 2, lower: math.Exp2(7.0 / 8), upper: 2},
			{v: 4, lower: math.Exp2(15.0 / 8), upper: 4},
			{v: 0.5, lower: math.Exp2(-9.0 / 8), upper: 0.5},
		} {
			buckets := histogramNonEmptyBucketsOf(histogramObserved(t, nil, tc.v))
			require.Len(t, buckets, 1, "v=%v", tc.v)
			require.InDelta(t, tc.upper, buckets[0].upper, 1e-12, "v=%v", tc.v)
			require.InDelta(t, tc.lower, buckets[0].lower, 1e-12, "v=%v", tc.v)
		}
	})

	t.Run("either side of a bound", func(t *testing.T) {
		below := histogramNonEmptyBucketsOf(histogramObserved(t, nil, math.Nextafter(1, 0)))
		on := histogramNonEmptyBucketsOf(histogramObserved(t, nil, 1))
		above := histogramNonEmptyBucketsOf(histogramObserved(t, nil, math.Nextafter(1, 2)))
		require.Equal(t, on[0], below[0], "just below the bound is in the same bucket")
		require.Equal(t, on[0].upper, above[0].lower, "just above the bound starts the next bucket")
		require.Greater(t, above[0].upper, on[0].upper)
	})

	// This checks the boundary table of the builder against the bounds that Prometheus
	// computes, for the bounds of many buckets, and the values next to them.
	t.Run("every bucket bound agrees with Prometheus", func(t *testing.T) {
		for index := int32(-8 * 40); index <= 8*40; index++ {
			ref := &histogram.FloatHistogram{
				Schema:          3,
				ZeroThreshold:   prometheus.DefNativeHistogramZeroThreshold,
				PositiveSpans:   []histogram.Span{{Offset: index, Length: 1}},
				PositiveBuckets: []float64{1},
			}
			it := ref.AllBucketIterator()
			require.True(t, it.Next())
			ub := it.At().Upper

			on := histogramNonEmptyBucketsOf(histogramObserved(t, nil, ub))
			require.Len(t, on, 1)
			require.Equal(t, ub, on[0].upper, "index %d: the bound itself belongs to its bucket", index)

			above := histogramNonEmptyBucketsOf(histogramObserved(t, nil, math.Nextafter(ub, math.Inf(1))))
			require.Equal(t, ub, above[0].lower, "index %d: the next value up is in the next bucket", index)
		}
	})

	t.Run("zero and the zero threshold", func(t *testing.T) {
		zt := prometheus.DefNativeHistogramZeroThreshold
		h := histogramObserved(t, nil, 0, math.Copysign(0, -1), zt, -zt, math.Nextafter(zt, 1), -math.Nextafter(zt, 1))
		require.Equal(t, 4.0, h.ZeroCount, "0, -0, +threshold and -threshold are in the zero bucket")
		require.Equal(t, 6.0, h.Count)
		require.Len(t, histogramNonEmptyBucketsOf(h), 3, "the zero bucket and one value on each side of it")
	})

	t.Run("negative values mirror the positive ones", func(t *testing.T) {
		pos := histogramNonEmptyBucketsOf(histogramObserved(t, nil, 3))
		neg := histogramNonEmptyBucketsOf(histogramObserved(t, nil, -3))
		require.Len(t, pos, 1)
		require.Len(t, neg, 1)
		require.Equal(t, -pos[0].upper, neg[0].lower)
		require.Equal(t, -pos[0].lower, neg[0].upper)

		// The bound itself, which is the lower bound of the negative bucket, belongs to it.
		onBound := histogramNonEmptyBucketsOf(histogramObserved(t, nil, -1))
		require.Equal(t, -1.0, onBound[0].lower)
	})

	t.Run("the range of float64", func(t *testing.T) {
		h := histogramObserved(t, nil, math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64)
		require.Equal(t, 3.0, h.Count)
	})

	t.Run("Count and Sum", func(t *testing.T) {
		h := histogramObserved(t, nil, 1, 2, 3.5, -4, 0)
		require.Equal(t, 5.0, h.Count)
		require.Equal(t, 2.5, h.Sum)
	})
}

func TestHistogramBuilder_nonFiniteValuesAreDropped(t *testing.T) {
	h := histogramObserved(t, nil, math.NaN(), math.Inf(1), math.Inf(-1), 7)
	require.Equal(t, 1.0, h.Count, "they are not counted")
	require.Equal(t, 7.0, h.Sum, "and they are not summed")
	require.Len(t, histogramNonEmptyBucketsOf(h), 1)

	h = histogramObserved(t, &syntax.BucketSpec{Bounds: []float64{1, 2}}, math.NaN(), math.Inf(1), math.Inf(-1))
	require.Equal(t, 0.0, h.Count)
	require.Empty(t, histogramNonEmptyBucketsOf(h))
}

func TestHistogramBuilder_empty(t *testing.T) {
	for name, spec := range map[string]*syntax.BucketSpec{
		"exponential": nil,
		"custom":      {Bounds: []float64{1, 2}},
	} {
		t.Run(name, func(t *testing.T) {
			h := histogramObserved(t, spec)
			require.Equal(t, 0.0, h.Count)
			require.Equal(t, 0.0, h.Sum)
			require.Empty(t, histogramNonEmptyBucketsOf(h))
		})
	}
}

func TestHistogramBuilder_custom(t *testing.T) {
	spec := &syntax.BucketSpec{Bounds: []float64{1, 2, 3}}
	h := histogramObserved(t, spec, -10, 0.5, 1, 1.5, 2, 3, 3.1, 1e9)

	require.True(t, h.UsesCustomBuckets())
	require.Equal(t, histogram.GaugeType, h.CounterResetHint)
	require.Equal(t, []float64{1, 2, 3}, h.CustomValues)
	require.Zero(t, h.ZeroCount)
	require.Zero(t, h.ZeroThreshold)
	require.Empty(t, h.NegativeBuckets)
	require.Equal(t, 8.0, h.Count)

	// (-Inf, 1], (1, 2], (2, 3], (3, +Inf). A value on a bound belongs to the bucket below it.
	require.Equal(t, []histogramBucketOf{
		{lower: math.Inf(-1), upper: 1, count: 3}, // -10, 0.5, 1
		{lower: 1, upper: 2, count: 2},            // 1.5, 2
		{lower: 2, upper: 3, count: 1},            // 3
		{lower: 3, upper: math.Inf(1), count: 2},  // 3.1, 1e9
	}, histogramNonEmptyBucketsOf(h))
}

func TestHistogramBuilder_customSharesItsBounds(t *testing.T) {
	spec := &syntax.BucketSpec{Bounds: []float64{1, 2, 3}}
	a := histogramObserved(t, spec, 1)
	b := histogramObserved(t, spec, 2)
	require.True(t, histogram.CustomBucketBoundsMatch(a.CustomValues, b.CustomValues))
	require.Same(t, &spec.Bounds[0], &a.CustomValues[0], "the bounds are read only and shared, not copied for each histogram")
}

func TestHistogramBuilder_spans(t *testing.T) {
	// The values land in the buckets of the indexes -2, -1, 0, 3, 4 and 10 of schema 3, which
	// give three spans: the first starts at -2, and the others start after a gap.
	var values []float64
	for _, index := range []int{-2, -1, 0, 3, 4, 10} {
		values = append(values, histogramValueInBucket(index))
	}
	h := histogramObserved(t, nil, values...)

	require.Equal(t, []histogram.Span{{Offset: -2, Length: 3}, {Offset: 2, Length: 2}, {Offset: 5, Length: 1}}, h.PositiveSpans)
	require.Equal(t, []float64{1, 1, 1, 1, 1, 1}, h.PositiveBuckets)
}

func TestHistogramBuilder_bucketLimit(t *testing.T) {
	// One value in each of n buckets of schema 3.
	valuesInBuckets := func(n int) []float64 {
		values := make([]float64, 0, n)
		for i := 0; i < n; i++ {
			values = append(values, histogramValueInBucket(i))
		}
		return values
	}

	t.Run("the limit is allowed", func(t *testing.T) {
		h := histogramObserved(t, nil, valuesInBuckets(maxHistogramBuckets)...)
		require.Len(t, histogramNonEmptyBucketsOf(h), maxHistogramBuckets)
	})

	t.Run("one more fails the query", func(t *testing.T) {
		b := newHistogramBuilder(nil)
		for _, v := range valuesInBuckets(maxHistogramBuckets + 1) {
			b.observe(v)
		}
		_, err := b.build()
		require.Error(t, err)
		require.ErrorIs(t, err, logqlerr.ErrLimit, "it is a limit, so it is served as a client error")
		require.ErrorContains(t, err, "160")
	})

	t.Run("the zero bucket and both signs count", func(t *testing.T) {
		values := append(valuesInBuckets(maxHistogramBuckets-2), 0, -1)
		require.Len(t, histogramNonEmptyBucketsOf(histogramObserved(t, nil, values...)), maxHistogramBuckets)

		b := newHistogramBuilder(nil)
		for _, v := range append(values, -2) {
			b.observe(v)
		}
		_, err := b.build()
		require.ErrorIs(t, err, logqlerr.ErrLimit)
	})

	t.Run("repeated values in a bucket count once", func(t *testing.T) {
		values := valuesInBuckets(maxHistogramBuckets)
		for i := 0; i < 5; i++ {
			values = append(values, values...)
		}
		require.Len(t, histogramNonEmptyBucketsOf(histogramObserved(t, nil, values...)), maxHistogramBuckets)
	})

	t.Run("a merged histogram is checked with the same function", func(t *testing.T) {
		require.NoError(t, checkHistogramBuckets(histogramObserved(t, nil, valuesInBuckets(maxHistogramBuckets)...)))

		// 160 buckets with the values of one set and 160 more with those of another.
		a := histogramObserved(t, nil, valuesInBuckets(maxHistogramBuckets)...)
		other := make([]float64, 0, maxHistogramBuckets)
		for _, v := range valuesInBuckets(maxHistogramBuckets) {
			other = append(other, -v)
		}
		b := histogramObserved(t, nil, other...)
		merged, _, _, err := a.Copy().Add(b)
		require.NoError(t, err)
		require.ErrorIs(t, checkHistogramBuckets(merged), logqlerr.ErrLimit)
	})

	t.Run("custom buckets have the bounds, so they are not limited", func(t *testing.T) {
		bounds := make([]float64, syntax.MaxBucketBounds)
		for i := range bounds {
			bounds[i] = float64(i)
		}
		h := histogramObserved(t, &syntax.BucketSpec{Bounds: bounds}, -1, 5, 20, 1e6)
		require.NoError(t, checkHistogramBuckets(h))
	})
}

// For any observations the counts in the buckets add up to Count, and Count and Sum are
// those of the finite observations.
func TestHistogramBuilder_countsAddUp(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	specs := map[string]struct {
		spec *syntax.BucketSpec
		// decades is the range of the magnitudes, in powers of ten. A histogram with exponential
		// buckets is limited in buckets, and each decade is about 27 of them, on each side of zero.
		decades float64
	}{
		"exponential": {spec: nil, decades: 2},
		"custom":      {spec: &syntax.BucketSpec{Bounds: []float64{-5, -0.1, 0, 0.001, 0.5, 1, 10, 1000}}, decades: 8},
	}
	for name, tc := range specs {
		spec := tc.spec
		t.Run(name, func(t *testing.T) {
			for round := 0; round < 200; round++ {
				var (
					values    []float64
					wantCount float64
					wantSum   float64
				)
				for i, n := 0, r.Intn(300); i < n; i++ {
					var v float64
					switch r.Intn(8) {
					case 0:
						v = 0
					case 1:
						v = math.NaN()
					case 2:
						v = math.Inf(1 - 2*r.Intn(2))
					default:
						// a logarithmic scale around 1, with both signs.
						v = math.Pow(10, (r.Float64()-0.5)*tc.decades) * float64(1-2*r.Intn(2))
					}
					values = append(values, v)
					if !math.IsNaN(v) && !math.IsInf(v, 0) {
						wantCount++
						wantSum += v
					}
				}

				h := histogramObserved(t, spec, values...)
				require.Equal(t, wantCount, h.Count)
				require.InDelta(t, wantSum, h.Sum, 1e-6)

				total := h.ZeroCount
				for _, b := range histogramNonEmptyBucketsOf(h) {
					total += b.count
				}
				// The zero bucket is part of the iteration, so it is counted once.
				if !h.UsesCustomBuckets() {
					total -= h.ZeroCount
				}
				require.Equal(t, wantCount, total)
			}
		})
	}
}
