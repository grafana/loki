package logql

import (
	"fmt"
	"math"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	HistogramMatrixType = "HistogramMatrix"

	// maxHistogramCount is the most observations that a histogram of the wire format holds, and the most that one
	// bucket does. The counts are added up as floats, which are whole numbers only up to it, so a count that is
	// more is not the count of the observations after it has been through ToFloat.
	maxHistogramCount = 1 << 53
)

// HistogramMatrix is the result of a query that returns histograms: the vector of each step, in order. It is
// what the shard of a histogram query returns to the query frontend, and the response to a user is never one.
//
// A promql.Matrix, which has the histograms of a series in Histograms, would be read by the codec of the API,
// which writes floats only and so would drop them. A type of its own makes it clear what is a response to a
// user and what is a part of a query. It keeps the steps, as the sketch matrices do, so that the matrices of
// the shards are put together one step at a time.
type HistogramMatrix []SampleVector

func (HistogramMatrix) String() string {
	return "HistogramMatrix()"
}

func (HistogramMatrix) Type() promql_parser.ValueType { return HistogramMatrixType }

// ToProto returns the matrix as it is sent from a querier to the query frontend. It fails for a sample that
// does not hold a histogram, and for one that the wire format cannot hold, see histogramToProto.
func (m HistogramMatrix) ToProto() (*logproto.HistogramMatrix, error) {
	values := make([]*logproto.HistogramVector, len(m))
	for i, vec := range m {
		samples := make([]*logproto.HistogramSample, len(vec))
		for j, s := range vec {
			if s.H == nil {
				return nil, fmt.Errorf("the sample %d of step %d of a histogram matrix has no histogram", j, i)
			}
			h, err := histogramToProto(s.H)
			if err != nil {
				return nil, err
			}
			metric := make([]*logproto.LabelPair, 0, s.Metric.Len())
			s.Metric.Range(func(l labels.Label) {
				metric = append(metric, &logproto.LabelPair{Name: l.Name, Value: l.Value})
			})
			samples[j] = &logproto.HistogramSample{H: h, TimestampMs: s.T, Metric: metric}
		}
		values[i] = &logproto.HistogramVector{Samples: samples}
	}
	return &logproto.HistogramMatrix{Values: values}, nil
}

// HistogramMatrixFromProto is the reverse of ToProto. It does not trust what it reads: a histogram that is not
// valid, or that has more buckets than a query may build, is an error.
func HistogramMatrixFromProto(proto *logproto.HistogramMatrix) (HistogramMatrix, error) {
	if proto == nil {
		return nil, fmt.Errorf("a histogram response has no matrix")
	}
	out := make(HistogramMatrix, len(proto.Values))
	for i, v := range proto.Values {
		vec := make(SampleVector, len(v.Samples))
		for j, s := range v.Samples {
			h, err := histogramFromProto(s.H)
			if err != nil {
				return nil, fmt.Errorf("the sample %d of step %d of a histogram matrix: %w", j, i, err)
			}
			b := labels.NewScratchBuilder(len(s.Metric))
			for _, p := range s.Metric {
				b.Add(p.Name, p.Value)
			}
			vec[j] = promql.Sample{T: s.TimestampMs, H: h, Metric: b.Labels()}
		}
		out[i] = vec
	}
	return out, nil
}

// histogramToProto returns the wire form of h, which has the counts of the buckets as integers. The counts of the
// histograms that a query builds, and of their sums, are whole numbers. It fails for one that is not, and does not
// round it, so that what a shard sends is what the frontend adds up.
func histogramToProto(h *histogram.FloatHistogram) (*logproto.NativeHistogram, error) {
	count, err := wholeCount(h.Count)
	if err != nil {
		return nil, fmt.Errorf("histogram count: %w", err)
	}
	zeroCount, err := wholeCount(h.ZeroCount)
	if err != nil {
		return nil, fmt.Errorf("histogram zero count: %w", err)
	}
	positive, err := bucketDeltas(h.PositiveBuckets)
	if err != nil {
		return nil, fmt.Errorf("histogram positive buckets: %w", err)
	}
	negative, err := bucketDeltas(h.NegativeBuckets)
	if err != nil {
		return nil, fmt.Errorf("histogram negative buckets: %w", err)
	}

	return &logproto.NativeHistogram{
		Schema:           h.Schema,
		CounterResetHint: uint32(h.CounterResetHint),
		ZeroThreshold:    h.ZeroThreshold,
		ZeroCount:        zeroCount,
		Count:            count,
		Sum:              h.Sum,
		NegativeSpans:    spansToProto(h.NegativeSpans),
		PositiveSpans:    spansToProto(h.PositiveSpans),
		NegativeDeltas:   negative,
		PositiveDeltas:   positive,
		CustomValues:     h.CustomValues,
	}, nil
}

// histogramFromProto is the reverse of histogramToProto.
//
// A histogram of a query has at most maxHistogramBuckets non-empty buckets, or at most syntax.MaxBucketBounds
// custom bounds, and one that has more was not built by a query. It is refused, as is a histogram that Prometheus
// does not accept, such as one whose spans do not match its buckets, or whose counts are negative. The frontend
// would otherwise sum whatever a response holds.
//
// The size of what it keeps is limited as well, see checkEncodedBuckets, and that is checked before the histogram
// is copied. Validate looks at the shape of a histogram and not at the values that the frontend relies on when it
// adds histograms up, see checkHistogramValues and checkZeroBucket.
func histogramFromProto(p *logproto.NativeHistogram) (*histogram.FloatHistogram, error) {
	if p == nil {
		return nil, fmt.Errorf("a sample has no histogram")
	}
	if p.CounterResetHint > uint32(histogram.GaugeType) {
		return nil, fmt.Errorf("unknown counter reset hint %d", p.CounterResetHint)
	}
	if len(p.CustomValues) > syntax.MaxBucketBounds {
		return nil, fmt.Errorf("a histogram has %d custom bounds, and at most %d are allowed", len(p.CustomValues), syntax.MaxBucketBounds)
	}
	if err := checkEncodedBuckets(p); err != nil {
		return nil, err
	}

	h := &histogram.Histogram{
		CounterResetHint: histogram.CounterResetHint(p.CounterResetHint),
		Schema:           p.Schema,
		ZeroThreshold:    p.ZeroThreshold,
		ZeroCount:        p.ZeroCount,
		Count:            p.Count,
		Sum:              p.Sum,
		NegativeSpans:    spansFromProto(p.NegativeSpans),
		PositiveSpans:    spansFromProto(p.PositiveSpans),
		NegativeBuckets:  p.NegativeDeltas,
		PositiveBuckets:  p.PositiveDeltas,
		CustomValues:     p.CustomValues,
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if err := checkHistogramValues(h); err != nil {
		return nil, err
	}

	fh := h.ToFloat(nil)
	if err := checkZeroBucket(fh); err != nil {
		return nil, err
	}
	if err := checkHistogramBuckets(fh); err != nil {
		return nil, err
	}
	return fh, nil
}

// checkEncodedBuckets fails when a side of p has more than maxHistogramBuckets buckets or spans.
//
// The buckets of a histogram of a query are its non-empty ones: the builder only makes those, and the sum of
// histograms compacts the empty ones away. So a side has no more of them than the limit of non-empty buckets,
// and each span holds at least one. Prometheus accepts any number of empty buckets, and of spans that hold none,
// as long as they match each other, and the limit of non-empty buckets does not count them. A response could
// otherwise make a histogram as large as the response itself, which ToFloat copies and the frontend adds up.
func checkEncodedBuckets(p *logproto.NativeHistogram) error {
	for _, side := range [...]struct {
		name           string
		spans, buckets int
	}{
		{"positive", len(p.PositiveSpans), len(p.PositiveDeltas)},
		{"negative", len(p.NegativeSpans), len(p.NegativeDeltas)},
	} {
		if side.spans > maxHistogramBuckets || side.buckets > maxHistogramBuckets {
			return fmt.Errorf("the %s side of a histogram has %d spans and %d buckets, and at most %d of either are allowed",
				side.name, side.spans, side.buckets, maxHistogramBuckets)
		}
	}
	return nil
}

// checkHistogramValues fails for a histogram that Prometheus accepts, that a query does not build, and that adding
// it to another can hang on, panic on, or silently get wrong. Validate has checked its shape: that the spans match
// the buckets, and that no count is negative.
//
//   - A span holds a bucket. Validate lets a span have none, and the addition of histograms indexes the first bucket
//     of a span that has the index of one that it adds, which panics. The builder does not make one, and the sum
//     of histograms compacts them away.
//   - The schema and the zero threshold are the ones that the builder uses. The frontend adds up the histograms of
//     the shards of one query, which have the same schema and the same threshold, so it never has to bring two
//     apart, and Prometheus does not do it well. A NaN threshold never equals another, so merging the zero buckets
//     loops for ever. A negative one, or an infinite one, gives a sum that is not the histogram of the observations.
//     So does a threshold that is inside a bucket, when the schemas of the two differ: observations are counted
//     in the zero bucket and in the bucket, and the sum has more of them than the two.
//   - The buckets of an exponential schema have an index that a float can have, see bucketIndexRange. The indexes
//     are added in 32 bits when histograms are added up, and one near the limit of them wraps around and panics.
//   - The custom bounds are finite, as the bounds of buckets(...) are. Validate lets the first one be -Inf.
//   - The count is the number of observations in the buckets, whether the sum is a number or not. Validate lets
//     a histogram with a NaN sum have more, which the producers of one do not make: a query does not observe NaN or
//     the infinities, and a sum can be NaN only because infinities of the sums of finite values cancel. The
//     count is at most maxHistogramCount, as histogramToProto has it.
func checkHistogramValues(h *histogram.Histogram) error {
	for _, side := range [...]struct {
		name  string
		spans []histogram.Span
	}{{"positive", h.PositiveSpans}, {"negative", h.NegativeSpans}} {
		for _, s := range side.spans {
			if s.Length == 0 {
				return fmt.Errorf("a span of the %s side holds no bucket", side.name)
			}
		}
	}

	if h.UsesCustomBuckets() {
		for _, v := range h.CustomValues {
			if math.IsInf(v, 0) {
				return fmt.Errorf("custom bound %v is not finite", v)
			}
		}
	} else {
		if h.Schema != histogramSchema {
			return fmt.Errorf("schema %d is not the schema %d of the histograms of a query", h.Schema, histogramSchema)
		}
		if h.ZeroThreshold != prometheus.DefNativeHistogramZeroThreshold {
			return fmt.Errorf("zero threshold %v is not the %v of the histograms of a query", h.ZeroThreshold, prometheus.DefNativeHistogramZeroThreshold)
		}
		if err := checkBucketIndexes("positive", h.Schema, h.PositiveSpans); err != nil {
			return err
		}
		if err := checkBucketIndexes("negative", h.Schema, h.NegativeSpans); err != nil {
			return err
		}
	}

	observations, ok := bucketObservations(h)
	if !ok {
		return fmt.Errorf("the buckets hold more observations than the wire format counts")
	}
	if observations != h.Count {
		return fmt.Errorf("the buckets hold %d observations, and the count is %d", observations, h.Count)
	}
	return nil
}

// checkZeroBucket fails when a bucket that holds observations is inside the zero bucket, which is when its upper
// bound is not above the zero threshold. Adding histograms up leaves out such a bucket, so its observations are
// lost from the sum. The builder puts the values that the zero bucket covers in it, so a query does not make one.
func checkZeroBucket(h *histogram.FloatHistogram) error {
	if h.UsesCustomBuckets() {
		return nil
	}
	for _, side := range [...]struct {
		name string
		it   histogram.BucketIterator[float64]
	}{
		{"positive", h.PositiveBucketIterator()},
		{"negative", h.NegativeBucketIterator()},
	} {
		for side.it.Next() {
			b := side.it.At()
			// The bounds of the negative buckets are negative, and the one that is nearer to zero is the upper one.
			nearest := b.Upper
			if side.name == "negative" {
				nearest = -b.Lower
			}
			if b.Count > 0 && nearest <= h.ZeroThreshold {
				return fmt.Errorf("a bucket of the %s side holds %v observations, and it is inside the zero bucket, which has the threshold %v",
					side.name, b.Count, h.ZeroThreshold)
			}
		}
	}
	return nil
}

// bucketObservations returns the number of observations in the buckets of h, the zero bucket included. It is false
// when they are more than maxHistogramCount. h has passed Validate, which has checked that the count of a bucket is
// not negative as it adds up the deltas, and that the sum of them does not wrap around, so neither does this.
func bucketObservations(h *histogram.Histogram) (uint64, bool) {
	total := h.ZeroCount
	if total > maxHistogramCount {
		return 0, false
	}
	for _, deltas := range [...][]int64{h.PositiveBuckets, h.NegativeBuckets} {
		var count int64
		for _, d := range deltas {
			count += d
			total += uint64(count)
			if total > maxHistogramCount {
				return 0, false
			}
		}
	}
	return total, true
}

// checkBucketIndexes fails when a span of a side holds a bucket that has an index that no float has. The first
// span starts at an index and each of the others at an offset after the end of the one before. Every span holds a
// bucket, see checkHistogramValues.
func checkBucketIndexes(side string, schema int32, spans []histogram.Span) error {
	lo, hi := bucketIndexRange(schema)
	var next int64 // the index after the last bucket of the span before
	for _, s := range spans {
		start := next + int64(s.Offset)
		next = start + int64(s.Length)
		if start < lo || next-1 > hi {
			return fmt.Errorf("a span of the %s side has the buckets %d to %d, and the indexes of schema %d are %d to %d",
				side, start, next-1, schema, lo, hi)
		}
	}
	return nil
}

// bucketIndexRange returns the lowest and the highest index of a bucket of an exponential schema that a float
// can be in, with a little to spare. The bucket i has the upper bound 2^(i/2^schema), and the floats are from
// 2^-1074 to just under 2^1024. A schema below 0 has a bucket for several exponents, so there are fewer.
func bucketIndexRange(schema int32) (lo, hi int64) {
	if schema >= 0 {
		return -(1075 << schema), 1024 << schema
	}
	return -(1075 >> -schema) - 1, (1024 >> -schema) + 1
}

// wholeCount returns v as a count. It fails for a number that is not one: negative, with a fraction, or more than
// maxHistogramCount.
func wholeCount(v float64) (uint64, error) {
	if v < 0 || v != math.Trunc(v) || v > maxHistogramCount {
		return 0, fmt.Errorf("%v is not a count the wire format can hold", v)
	}
	return uint64(v), nil
}

// bucketDeltas returns the counts of buckets as the difference of each to the one before. The first one is the
// difference to zero.
func bucketDeltas(counts []float64) ([]int64, error) {
	if len(counts) == 0 {
		return nil, nil
	}
	deltas := make([]int64, len(counts))
	var prev int64
	for i, c := range counts {
		n, err := wholeCount(c)
		if err != nil {
			return nil, err
		}
		deltas[i] = int64(n) - prev
		prev = int64(n)
	}
	return deltas, nil
}

func spansToProto(spans []histogram.Span) []*logproto.BucketSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]*logproto.BucketSpan, len(spans))
	for i, s := range spans {
		out[i] = &logproto.BucketSpan{Offset: s.Offset, Length: s.Length}
	}
	return out
}

func spansFromProto(spans []*logproto.BucketSpan) []histogram.Span {
	if len(spans) == 0 {
		return nil
	}
	out := make([]histogram.Span, len(spans))
	for i, s := range spans {
		out[i] = histogram.Span{Offset: s.Offset, Length: s.Length}
	}
	return out
}

// JoinHistogramVector joins the results of stepEvaluator into a HistogramMatrix. It is JoinSampleVector for a query
// whose result is a histogram, which only the part of a query that a shard runs is.
//
// It limits the series as JoinSampleVector does: the query fails when the steps together have more than maxSeries
// distinct series. Zero is no limit.
func JoinHistogramVector(next bool, r StepResult, stepEvaluator StepEvaluator, maxSeries int) (promql_parser.Value, error) {
	if err := stepEvaluator.Error(); err != nil {
		return nil, err
	}

	var (
		result = HistogramMatrix{}
		series = map[uint64]struct{}{}
	)
	for next {
		vec := r.SampleVector()
		for _, s := range vec {
			if s.H == nil {
				return nil, fmt.Errorf("%w: a query that returns histograms has a sample without one: %s", logqlerr.ErrParse, s.Metric)
			}
			series[s.Metric.Hash()] = struct{}{}
		}
		if maxSeries > 0 && len(series) > maxSeries {
			return nil, logqlerr.NewSeriesLimitError(maxSeries)
		}
		// The evaluator may hand out the same vector for the next step, as the range iterator of histogram_over_time
		// does. The histograms in it are new for each step, so a copy of the vector is enough.
		result = append(result, slices.Clone(SampleVector(vec)))

		next, _, r = stepEvaluator.Next()
		if err := stepEvaluator.Error(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// HistogramMatrixStepEvaluator steps through a matrix of histogram vectors, one evaluation timestamp at a time.
type HistogramMatrixStepEvaluator = SketchMatrixStepEvaluator[SampleVector]

func NewHistogramMatrixStepEvaluator(m HistogramMatrix, params Params) *HistogramMatrixStepEvaluator {
	return newSketchMatrixStepEvaluator(m, params, HistogramMatrixType)
}
