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

	// count is the number of the observations. It is above the total of the buckets for each NaN, which is in none.
	count float64
	// sum is summed with compensation, as sum_over_time and the sum aggregation do, so that the sum of a
	// histogram is the same whether one builder sees all the values or the histograms of their parts are
	// summed afterwards. A plain sum loses the 1 of 1e16 + 1 - 1e16, and the other forms do not.
	sum kahanSum
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

// observe adds a value to the histogram. Every value is counted and added to the sum, NaN and the infinities
// too, as sum_over_time, avg_over_time and the other functions of an unwrapped value do, so that Count is the
// number of the lines and the average is the one of avg_over_time. Where it goes, as in Prometheus:
//
//   - Custom buckets have no case of their own. A search for the first bound that is not below v puts +Inf
//     and NaN, which no bound is below, in the last bucket, and -Inf in the first, as for a classic histogram.
//   - Exponential buckets have ±Inf in the bucket above the one of math.MaxFloat64, see exponentialBucketIndex,
//     as client_golang has it. NaN is in no bucket, so Count is above the total of the buckets, and the sum
//     is NaN, which is what the functions that read a histogram look for.
func (b *histogramBuilder) observe(v float64) {
	b.count++
	b.sum.addValue(v)

	if b.bounds != nil {
		// The first bound that is not below v. It is len(bounds) for a value above the last bound, and for NaN.
		b.buckets[sort.SearchFloat64s(b.bounds, v)]++
		return
	}

	switch {
	case math.IsNaN(v):
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
//
// The bucket of math.MaxFloat64 is the last one that a number can be in, and its upper bound is
// math.MaxFloat64. +Inf is in the bucket above it, which has the upper bound +Inf, as it is in client_golang.
func exponentialBucketIndex(v float64) int {
	if math.IsInf(v, 1) {
		return exponentialBucketIndex(math.MaxFloat64) + 1
	}
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
		Sum:              b.sum.result(),
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

// checkBucketLimit fails with logqlerr.ErrHistogramBucketLimit, and not with ErrLimit, which is the limit of series.
// The message starts with the text of the error, which is what tells it apart where the error has crossed from a
// querier to the frontend as an HTTP response, and has lost its type, see server.ClassifyFailure.
func checkBucketLimit(nonEmpty int) error {
	if nonEmpty > maxHistogramBuckets {
		return fmt.Errorf("%w: a histogram has %d non-empty buckets, and at most %d are allowed. "+
			"Use buckets(...) to choose the bounds, or narrow the query",
			logqlerr.ErrHistogramBucketLimit, nonEmpty, maxHistogramBuckets)
	}
	return nil
}
