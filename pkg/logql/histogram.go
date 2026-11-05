package logql

import (
	"fmt"
	"math"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/histogram"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	// histogramSchema is the schema of the default, exponential buckets. Every doubling of a value is
	// divided into 2^3 = 8 buckets, so a bucket is 9% wider than the one before it and a value is
	// placed within 4.4% of where it is, before an interpolation.
	histogramSchema = 3

	// maxHistogramBuckets is the most non-empty buckets a histogram may have. A query that goes over
	// it fails, and does not lose resolution, so that its outcome does not depend on how it is sharded.
	maxHistogramBuckets = 160
)

// histogramBucketsPerDoubling is the number of buckets of the schema between a power of two and the next.
const histogramBucketsPerDoubling = 1 << histogramSchema

// histogramBounds are the bucket bounds of the schema inside [0.5, 1]: histogramBounds[t] is about
// 2^((t-m)/m) for m buckets per doubling, from 0.5 to 1. The bounds of the other octaves are these
// ones times a power of two.
//
// They are read from Prometheus, and not computed again, so that a value that is on a bound, or the
// one next to it, is in the same bucket here as it is for every function that reads the histogram.
// The bound of an irrational power of two is rounded, and a table that is computed differently can
// differ by the last bit.
var histogramBounds = func() []float64 {
	m := histogramBucketsPerDoubling
	reference := &histogram.FloatHistogram{
		Schema:          histogramSchema,
		PositiveSpans:   []histogram.Span{{Offset: -int32(m), Length: uint32(m) + 1}},
		PositiveBuckets: make([]float64, m+1),
	}
	bounds := make([]float64, 0, m+1)
	for it := reference.PositiveBucketIterator(); it.Next(); {
		bounds = append(bounds, it.At().Upper)
	}
	return bounds
}()

// histogramBuilder collects the observations of a window into a native histogram.
//
// The buckets are either exponential, the default, or custom with the upper bounds of a bucket spec.
// A bucket includes its upper bound, as the buckets of a classic histogram do.
type histogramBuilder struct {
	// bounds are the upper bounds of the custom buckets. They are nil for exponential buckets. The
	// slice is shared by the histograms of the query and must not be changed.
	bounds []float64

	// buckets holds the number of observations by bucket index. For exponential buckets they are the
	// observations above the zero bucket, and negBuckets those below it. For custom buckets index i
	// is the bucket of the bound i, and the index len(bounds) is the one above the last bound.
	buckets    map[int]float64
	negBuckets map[int]float64
	zeroCount  float64

	count, sum float64
}

// newHistogramBuilder returns a builder for the buckets of spec. A nil spec selects the exponential buckets.
func newHistogramBuilder(spec *syntax.BucketSpec) *histogramBuilder {
	b := &histogramBuilder{buckets: map[int]float64{}}
	if spec != nil {
		b.bounds = spec.Bounds
	} else {
		b.negBuckets = map[int]float64{}
	}
	return b
}

// observe adds a value to the histogram. NaN and the infinities are not observed: they are not
// counted, and they have no bucket that would not skew a quantile.
func (b *histogramBuilder) observe(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	b.count++
	b.sum += v

	if b.bounds != nil {
		// The first bound that is not below v. It is len(bounds) for a value above the last bound.
		b.buckets[sort.SearchFloat64s(b.bounds, v)]++
		return
	}

	switch {
	case math.Abs(v) <= prometheus.DefNativeHistogramZeroThreshold:
		b.zeroCount++
	case v > 0:
		b.buckets[exponentialBucketIndex(v)]++
	default:
		b.negBuckets[exponentialBucketIndex(-v)]++
	}
}

// exponentialBucketIndex returns the index of the bucket of schema histogramSchema that holds v, which
// must be positive. The bucket i holds the values above 2^((i-1)/8) up to 2^(i/8), including that bound.
//
// It splits v into a fraction in [0.5, 1) and a power of two, which are both exact, and finds the fraction
// in a table of the bounds. Taking a logarithm instead would round a value that is on a bound to either side.
func exponentialBucketIndex(v float64) int {
	frac, exp := math.Frexp(v)
	t := sort.SearchFloat64s(histogramBounds, frac)
	return (exp-1)*histogramBucketsPerDoubling + t
}

// build returns the histogram of the observations. It fails when the histogram has more than
// maxHistogramBuckets non-empty buckets.
func (b *histogramBuilder) build() (*histogram.FloatHistogram, error) {
	h := &histogram.FloatHistogram{
		// A window is not a counter: the histogram of one window is not a part of the next one.
		CounterResetHint: histogram.GaugeType,
		Count:            b.count,
		Sum:              b.sum,
	}

	if b.bounds != nil {
		h.Schema = histogram.CustomBucketsSchema
		h.CustomValues = b.bounds
		h.PositiveSpans, h.PositiveBuckets = spansOf(b.buckets)
		return h, nil
	}

	nonEmpty := len(b.buckets) + len(b.negBuckets)
	if b.zeroCount > 0 {
		nonEmpty++
	}
	if err := checkBucketLimit(nonEmpty); err != nil {
		return nil, err
	}

	h.Schema = histogramSchema
	h.ZeroThreshold = prometheus.DefNativeHistogramZeroThreshold
	h.ZeroCount = b.zeroCount
	h.PositiveSpans, h.PositiveBuckets = spansOf(b.buckets)
	h.NegativeSpans, h.NegativeBuckets = spansOf(b.negBuckets)
	return h, nil
}

// spansOf returns the spans and the counts of the buckets by index. The first span starts at the
// index of the lowest bucket, and each of the others starts after the gap of empty buckets that is
// between it and the span before.
func spansOf(buckets map[int]float64) ([]histogram.Span, []float64) {
	if len(buckets) == 0 {
		return nil, nil
	}
	indexes := make([]int, 0, len(buckets))
	for i := range buckets {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)

	var (
		spans  []histogram.Span
		counts = make([]float64, 0, len(indexes))
		last   int
	)
	for n, i := range indexes {
		switch {
		case n == 0:
			spans = append(spans, histogram.Span{Offset: int32(i), Length: 1})
		case i == last+1:
			spans[len(spans)-1].Length++
		default:
			spans = append(spans, histogram.Span{Offset: int32(i - last - 1), Length: 1})
		}
		counts = append(counts, buckets[i])
		last = i
	}
	return spans, counts
}

// checkHistogramBuckets fails when h has more than maxHistogramBuckets non-empty buckets. Custom
// buckets are limited by the number of their bounds instead, so they always pass.
//
// The builder and the sum of histograms use it, so that a histogram of a shard and the merge of
// the histograms of all the shards fail in the same cases.
func checkHistogramBuckets(h *histogram.FloatHistogram) error {
	if h.UsesCustomBuckets() {
		return nil
	}
	n := 0
	if h.ZeroCount > 0 {
		n++
	}
	for _, c := range h.PositiveBuckets {
		if c > 0 {
			n++
		}
	}
	for _, c := range h.NegativeBuckets {
		if c > 0 {
			n++
		}
	}
	return checkBucketLimit(n)
}

func checkBucketLimit(nonEmpty int) error {
	if nonEmpty > maxHistogramBuckets {
		return fmt.Errorf("%w: a histogram has %d non-empty buckets, and at most %d are allowed. "+
			"Use buckets(...) to choose the bounds, or narrow the query",
			logqlerr.ErrLimit, nonEmpty, maxHistogramBuckets)
	}
	return nil
}
