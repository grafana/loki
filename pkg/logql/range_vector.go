package logql

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logql/vector"
)

// BatchRangeVectorAggregator aggregates samples for a given range of samples.
// It receives the current milliseconds timestamp and the list of point within
// the range.
type BatchRangeVectorAggregator func([]promql.Point) float64

// RangeStreamingAgg streaming aggregates sample for each sample
type RangeStreamingAgg interface {
	// agg func works inside the Next func of RangeVectorIterator, agg used to agg each sample.
	// agg will calculate the intermediate result after streaming agg each sample and try to save an aggregate value instead of keeping all samples.
	agg(sample promql.Point)
	// at func works inside the At func of RangeVectorIterator, get the intermediate result of agg func to provide the final value for At func of RangeVectorIterator
	at() float64
}

// RangeVectorIterator iterates through a range of samples.
// To fetch the current vector use `At` with a `BatchRangeVectorAggregator` or `RangeStreamingAgg`.
type RangeVectorIterator interface {
	Next() bool
	At() (int64, promql.Vector)
	Close() error
	Error() error
}

func newRangeVectorIterator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64) (RangeVectorIterator, error) {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}
	var overlap bool
	if selRange >= step && start != end {
		overlap = true
	}
	if !overlap {
		_, err := streamingAggregator(expr)
		if err != nil {
			return nil, err
		}
		return &streamRangeVectorIterator{
			iter:     it,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			r:        expr,
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		}, nil
	}
	vectorAggregator, err := aggregator(expr)
	if err != nil {
		return nil, err
	}
	return &batchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		window:   map[string]*promql.Series{},
		agg:      vectorAggregator,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}, nil
}

//batch

type batchRangeVectorIterator struct {
	iter                                 iter.PeekingSampleIterator
	selRange, step, end, current, offset int64
	window                               map[string]*promql.Series
	metrics                              map[string]labels.Labels
	at                                   []promql.Sample
	agg                                  BatchRangeVectorAggregator
}

func (r *batchRangeVectorIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	// load samples
	r.popBack(rangeStart)
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *batchRangeVectorIterator) Close() error {
	return r.iter.Close()
}

func (r *batchRangeVectorIterator) Error() error {
	return r.iter.Error()
}

// popBack removes all entries out of the current window from the back.
func (r *batchRangeVectorIterator) popBack(newStart int64) {
	// possible improvement: if there is no overlap we can just remove all.
	for fp := range r.window {
		lastPoint := 0
		remove := false
		for i, p := range r.window[fp].Points {
			if p.T <= newStart {
				lastPoint = i
				remove = true
				continue
			}
			break
		}
		if remove {
			r.window[fp].Points = r.window[fp].Points[lastPoint+1:]
		}
		if len(r.window[fp].Points) == 0 {
			s := r.window[fp]
			delete(r.window, fp)
			putSeries(s)
		}
	}
}

// load the next sample range window.
func (r *batchRangeVectorIterator) load(start, end int64) {
	for lbs, sample, hasNext := r.iter.Peek(); hasNext; lbs, sample, hasNext = r.iter.Peek() {
		if sample.Timestamp > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.Timestamp <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var series *promql.Series
		var ok bool
		series, ok = r.window[lbs]
		if !ok {
			var metric labels.Labels
			if metric, ok = r.metrics[lbs]; !ok {
				var err error
				metric, err = promql_parser.ParseMetric(lbs)
				if err != nil {
					_ = r.iter.Next()
					continue
				}
				r.metrics[lbs] = metric
			}

			series = getSeries()
			series.Metric = metric
			r.window[lbs] = series
		}
		p := promql.Point{
			T: sample.Timestamp,
			V: sample.Value,
		}
		series.Points = append(series.Points, p)
		_ = r.iter.Next()
	}
}
func (r *batchRangeVectorIterator) At() (int64, promql.Vector) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		r.at = append(r.at, promql.Sample{
			Point: promql.Point{
				V: r.agg(series.Points),
				T: ts,
			},
			Metric: series.Metric,
		})
	}
	return ts, r.at
}

var seriesPool sync.Pool

func getSeries() *promql.Series {
	if r := seriesPool.Get(); r != nil {
		s := r.(*promql.Series)
		s.Points = s.Points[:0]
		return s
	}
	return &promql.Series{
		Points: make([]promql.Point, 0, 1024),
	}
}

func putSeries(s *promql.Series) {
	seriesPool.Put(s)
}

func aggregator(r *syntax.RangeAggregationExpr) (BatchRangeVectorAggregator, error) {
	switch r.Operation {
	case syntax.OpRangeTypeRate:
		return rateLogs(r.Left.Interval, r.Left.Unwrap != nil), nil
	case syntax.OpRangeTypeRateCounter:
		return rateCounter(r.Left.Interval), nil
	case syntax.OpRangeTypeCount:
		return countOverTime, nil
	case syntax.OpRangeTypeBytesRate:
		return rateLogBytes(r.Left.Interval), nil
	case syntax.OpRangeTypeBytes, syntax.OpRangeTypeSum:
		return sumOverTime, nil
	case syntax.OpRangeTypeAvg:
		return avgOverTime, nil
	case syntax.OpRangeTypeMax:
		return maxOverTime, nil
	case syntax.OpRangeTypeMin:
		return minOverTime, nil
	case syntax.OpRangeTypeStddev:
		return stddevOverTime, nil
	case syntax.OpRangeTypeStdvar:
		return stdvarOverTime, nil
	case syntax.OpRangeTypeQuantile:
		return quantileOverTime(*r.Params), nil
	case syntax.OpRangeTypeFirst:
		return first, nil
	case syntax.OpRangeTypeLast:
		return last, nil
	case syntax.OpRangeTypeAbsent:
		return one, nil
	default:
		return nil, fmt.Errorf(syntax.UnsupportedErr, r.Operation)
	}
}

// rateLogs calculates the per-second rate of log lines or values extracted
// from log lines
func rateLogs(selRange time.Duration, computeValues bool) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		if !computeValues {
			return float64(len(samples)) / selRange.Seconds()
		}
		var result float64
		for _, sample := range samples {
			result += sample.V
		}
		return result / selRange.Seconds()
	}
}

// rateCounter calculates the per-second rate of values extracted from log lines
// and treat them like a "counter" metric.
func rateCounter(selRange time.Duration) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		return extrapolatedRate(samples, selRange, true, true)
	}
}

// extrapolatedRate function is taken from prometheus code promql/functions.go:59
// extrapolatedRate is a utility function for rate/increase/delta.
// It calculates the rate (allowing for counter resets if isCounter is true),
// extrapolates if the first/last sample is close to the boundary, and returns
// the result as either per-second (if isRate is true) or overall.
func extrapolatedRate(samples []promql.Point, selRange time.Duration, isCounter, isRate bool) float64 {
	// No sense in trying to compute a rate without at least two points. Drop
	// this Vector element.
	if len(samples) < 2 {
		return 0
	}
	var (
		rangeStart = samples[0].T - durationMilliseconds(selRange)
		rangeEnd   = samples[len(samples)-1].T
	)

	resultValue := samples[len(samples)-1].V - samples[0].V
	if isCounter {
		var lastValue float64
		for _, sample := range samples {
			if sample.V < lastValue {
				resultValue += lastValue
			}
			lastValue = sample.V
		}
	}

	// Duration between first/last samples and boundary of range.
	durationToStart := float64(samples[0].T-rangeStart) / 1000
	durationToEnd := float64(rangeEnd-samples[len(samples)-1].T) / 1000

	sampledInterval := float64(samples[len(samples)-1].T-samples[0].T) / 1000
	averageDurationBetweenSamples := sampledInterval / float64(len(samples)-1)

	if isCounter && resultValue > 0 && samples[0].V >= 0 {
		// Counters cannot be negative. If we have any slope at
		// all (i.e. resultValue went up), we can extrapolate
		// the zero point of the counter. If the duration to the
		// zero point is shorter than the durationToStart, we
		// take the zero point as the start of the series,
		// thereby avoiding extrapolation to negative counter
		// values.
		durationToZero := sampledInterval * (samples[0].V / resultValue)
		if durationToZero < durationToStart {
			durationToStart = durationToZero
		}
	}

	// If the first/last samples are close to the boundaries of the range,
	// extrapolate the result. This is as we expect that another sample
	// will exist given the spacing between samples we've seen thus far,
	// with an allowance for noise.
	extrapolationThreshold := averageDurationBetweenSamples * 1.1
	extrapolateToInterval := sampledInterval

	if durationToStart < extrapolationThreshold {
		extrapolateToInterval += durationToStart
	} else {
		extrapolateToInterval += averageDurationBetweenSamples / 2
	}
	if durationToEnd < extrapolationThreshold {
		extrapolateToInterval += durationToEnd
	} else {
		extrapolateToInterval += averageDurationBetweenSamples / 2
	}
	resultValue = resultValue * (extrapolateToInterval / sampledInterval)
	if isRate {
		seconds := selRange.Seconds()
		resultValue = resultValue / seconds
	}

	return resultValue
}

func durationMilliseconds(d time.Duration) int64 {
	return int64(d / (time.Millisecond / time.Nanosecond))
}

// rateLogBytes calculates the per-second rate of log bytes.
func rateLogBytes(selRange time.Duration) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		return sumOverTime(samples) / selRange.Seconds()
	}
}

// countOverTime counts the amount of log lines.
func countOverTime(samples []promql.Point) float64 {
	return float64(len(samples))
}

func sumOverTime(samples []promql.Point) float64 {
	var sum float64
	for _, v := range samples {
		sum += v.V
	}
	return sum
}

func avgOverTime(samples []promql.Point) float64 {
	var mean, count float64
	for _, v := range samples {
		count++
		if math.IsInf(mean, 0) {
			if math.IsInf(v.V, 0) && (mean > 0) == (v.V > 0) {
				// The `mean` and `v.V` values are `Inf` of the same sign.  They
				// can't be subtracted, but the value of `mean` is correct
				// already.
				continue
			}
			if !math.IsInf(v.V, 0) && !math.IsNaN(v.V) {
				// At this stage, the mean is an infinite. If the added
				// value is neither an Inf or a Nan, we can keep that mean
				// value.
				// This is required because our calculation below removes
				// the mean value, which would look like Inf += x - Inf and
				// end up as a NaN.
				continue
			}
		}
		mean += v.V/count - mean/count
	}
	return mean
}

func maxOverTime(samples []promql.Point) float64 {
	max := samples[0].V
	for _, v := range samples {
		if v.V > max || math.IsNaN(max) {
			max = v.V
		}
	}
	return max
}

func minOverTime(samples []promql.Point) float64 {
	min := samples[0].V
	for _, v := range samples {
		if v.V < min || math.IsNaN(min) {
			min = v.V
		}
	}
	return min
}

func stdvarOverTime(samples []promql.Point) float64 {
	var aux, count, mean float64
	for _, v := range samples {
		count++
		delta := v.V - mean
		mean += delta / count
		aux += delta * (v.V - mean)
	}
	return aux / count
}

func stddevOverTime(samples []promql.Point) float64 {
	var aux, count, mean float64
	for _, v := range samples {
		count++
		delta := v.V - mean
		mean += delta / count
		aux += delta * (v.V - mean)
	}
	return math.Sqrt(aux / count)
}

func quantileOverTime(q float64) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		values := make(vector.HeapByMaxValue, 0, len(samples))
		for _, v := range samples {
			values = append(values, promql.Sample{Point: promql.Point{V: v.V}})
		}
		return quantile(q, values)
	}
}

// quantile calculates the given quantile of a vector of samples.
//
// The Vector will be sorted.
// If 'values' has zero elements, NaN is returned.
// If q<0, -Inf is returned.
// If q>1, +Inf is returned.
func quantile(q float64, values vector.HeapByMaxValue) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}
	sort.Sort(values)

	n := float64(len(values))
	// When the quantile lies between two samples,
	// we use a weighted average of the two samples.
	rank := q * (n - 1)

	lowerIndex := math.Max(0, math.Floor(rank))
	upperIndex := math.Min(n-1, lowerIndex+1)

	weight := rank - math.Floor(rank)
	return values[int(lowerIndex)].V*(1-weight) + values[int(upperIndex)].V*weight
}

func first(samples []promql.Point) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	return samples[0].V
}

func last(samples []promql.Point) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	return samples[len(samples)-1].V
}

func one(samples []promql.Point) float64 {
	return 1.0
}

// streaming range agg
type streamRangeVectorIterator struct {
	iter                                 iter.PeekingSampleIterator
	selRange, step, end, current, offset int64
	windowRangeAgg                       map[string]RangeStreamingAgg
	r                                    *syntax.RangeAggregationExpr
	metrics                              map[string]labels.Labels
	at                                   []promql.Sample
	agg                                  BatchRangeVectorAggregator
}

func (r *streamRangeVectorIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	// load samples

	r.windowRangeAgg = make(map[string]RangeStreamingAgg, 0)
	r.metrics = map[string]labels.Labels{}
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *streamRangeVectorIterator) Close() error {
	return r.iter.Close()
}

func (r *streamRangeVectorIterator) Error() error {
	return r.iter.Error()
}

// load the next sample range window.
func (r *streamRangeVectorIterator) load(start, end int64) {
	for lbs, sample, hasNext := r.iter.Peek(); hasNext; lbs, sample, hasNext = r.iter.Peek() {
		if sample.Timestamp > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.Timestamp <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var rangeAgg RangeStreamingAgg
		var ok bool
		rangeAgg, ok = r.windowRangeAgg[lbs]
		if !ok {
			var metric labels.Labels
			if _, ok = r.metrics[lbs]; !ok {
				var err error
				metric, err = promql_parser.ParseMetric(lbs)
				if err != nil {
					_ = r.iter.Next()
					continue
				}
				r.metrics[lbs] = metric
			}

			// never err here ,we have check error at evaluator.go rangeAggEvaluator() func
			rangeAgg, _ = streamingAggregator(r.r)
			r.windowRangeAgg[lbs] = rangeAgg
		}
		p := promql.Point{
			T: sample.Timestamp,
			V: sample.Value,
		}
		rangeAgg.agg(p)
		_ = r.iter.Next()
	}
}

func (r *streamRangeVectorIterator) At() (int64, promql.Vector) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.windowRangeAgg))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for lbs, rangeAgg := range r.windowRangeAgg {
		r.at = append(r.at, promql.Sample{
			Point: promql.Point{
				V: rangeAgg.at(),
				T: ts,
			},
			Metric: r.metrics[lbs],
		})
	}
	return ts, r.at
}

func streamingAggregator(r *syntax.RangeAggregationExpr) (RangeStreamingAgg, error) {
	switch r.Operation {
	case syntax.OpRangeTypeRate:
		return newRateLogs(r.Left.Interval, r.Left.Unwrap != nil), nil
	case syntax.OpRangeTypeRateCounter:
		return &RateCounterOverTime{selRange: r.Left.Interval, samples: make([]promql.Point, 0)}, nil
	case syntax.OpRangeTypeCount:
		return &CountOverTime{}, nil
	case syntax.OpRangeTypeBytesRate:
		return &RateLogBytesOverTime{selRange: r.Left.Interval}, nil
	case syntax.OpRangeTypeBytes, syntax.OpRangeTypeSum:
		return &SumOverTime{}, nil
	case syntax.OpRangeTypeAvg:
		return &AvgOverTime{}, nil
	case syntax.OpRangeTypeMax:
		return &MaxOverTime{max: math.NaN()}, nil
	case syntax.OpRangeTypeMin:
		return &MinOverTime{min: math.NaN()}, nil
	case syntax.OpRangeTypeStddev:
		return &StddevOverTime{}, nil
	case syntax.OpRangeTypeStdvar:
		return &StdvarOverTime{}, nil
	case syntax.OpRangeTypeQuantile:
		return &QuantileOverTime{q: *r.Params, values: make(vector.HeapByMaxValue, 0)}, nil
	case syntax.OpRangeTypeFirst:
		return &FirstOverTime{}, nil
	case syntax.OpRangeTypeLast:
		return &LastOverTime{}, nil
	case syntax.OpRangeTypeAbsent:
		return &OneOverTime{}, nil
	default:
		return nil, fmt.Errorf(syntax.UnsupportedErr, r.Operation)
	}
}

func newRateLogs(selRange time.Duration, computeValues bool) RangeStreamingAgg {
	return &RateLogsOverTime{
		selRange:      selRange,
		computeValues: computeValues,
	}
}

// rateLogs calculates the per-second rate of log lines or values extracted
// from log lines
type RateLogsOverTime struct {
	selRange      time.Duration
	val           float64
	count         float64
	computeValues bool
}

func (a *RateLogsOverTime) agg(sample promql.Point) {
	a.count++
	a.val += sample.V
}

func (a *RateLogsOverTime) at() float64 {
	if !a.computeValues {
		return a.count / a.selRange.Seconds()
	}
	return a.val / a.selRange.Seconds()
}

// rateCounter calculates the per-second rate of values extracted from log lines
// and treat them like a "counter" metric.
type RateCounterOverTime struct {
	samples  []promql.Point
	selRange time.Duration
}

func (a *RateCounterOverTime) agg(sample promql.Point) {
	a.samples = append(a.samples, sample)
}

func (a *RateCounterOverTime) at() float64 {
	return extrapolatedRate(a.samples, a.selRange, true, true)
}

// rateLogBytes calculates the per-second rate of log bytes.
type RateLogBytesOverTime struct {
	sum      float64
	selRange time.Duration
}

func (a *RateLogBytesOverTime) agg(sample promql.Point) {
	a.sum += sample.V
}

func (a *RateLogBytesOverTime) at() float64 {
	return a.sum / a.selRange.Seconds()
}

type CountOverTime struct {
	count float64
}

func (a *CountOverTime) agg(sample promql.Point) {
	a.count++
}

func (a *CountOverTime) at() float64 {
	return a.count
}

type SumOverTime struct {
	sum float64
}

func (a *SumOverTime) agg(sample promql.Point) {
	a.sum += sample.V
}

func (a *SumOverTime) at() float64 {
	return a.sum
}

type AvgOverTime struct {
	mean, count float64
}

func (a *AvgOverTime) agg(sample promql.Point) {
	a.count++
	if math.IsInf(a.mean, 0) {
		if math.IsInf(sample.V, 0) && (a.mean > 0) == (sample.V > 0) {
			// The `mean` and `v.V` values are `Inf` of the same sign.  They
			// can't be subtracted, but the value of `mean` is correct
			// already.
			return
		}
		if !math.IsInf(sample.V, 0) && !math.IsNaN(sample.V) {
			// At this stage, the mean is an infinite. If the added
			// value is neither an Inf or a Nan, we can keep that mean
			// value.
			// This is required because our calculation below removes
			// the mean value, which would look like Inf += x - Inf and
			// end up as a NaN.
			return
		}
	}
	a.mean += sample.V/a.count - a.mean/a.count
}

func (a *AvgOverTime) at() float64 {
	return a.mean
}

type MaxOverTime struct {
	max float64
}

func (a *MaxOverTime) agg(sample promql.Point) {
	if sample.V > a.max || math.IsNaN(a.max) {
		a.max = sample.V
	}
}

func (a *MaxOverTime) at() float64 {
	return a.max
}

type MinOverTime struct {
	min float64
}

func (a *MinOverTime) agg(sample promql.Point) {
	if sample.V < a.min || math.IsNaN(a.min) {
		a.min = sample.V
	}
}

func (a *MinOverTime) at() float64 {
	return a.min
}

type StdvarOverTime struct {
	aux, count, mean float64
}

func (a *StdvarOverTime) agg(sample promql.Point) {
	a.count++
	delta := sample.V - a.mean
	a.mean += delta / a.count
	a.aux += delta * (sample.V - a.mean)
}

func (a *StdvarOverTime) at() float64 {
	return a.aux / a.count
}

type StddevOverTime struct {
	aux, count, mean float64
}

func (a *StddevOverTime) agg(sample promql.Point) {
	a.count++
	delta := sample.V - a.mean
	a.mean += delta / a.count
	a.aux += delta * (sample.V - a.mean)
}

func (a *StddevOverTime) at() float64 {
	return math.Sqrt(a.aux / a.count)
}

type QuantileOverTime struct {
	q      float64
	values vector.HeapByMaxValue
}

func (a *QuantileOverTime) agg(sample promql.Point) {
	a.values = append(a.values, promql.Sample{Point: promql.Point{V: sample.V}})

}

func (a *QuantileOverTime) at() float64 {
	return quantile(a.q, a.values)
}

type FirstOverTime struct {
	v       float64
	hasData bool
}

func (a *FirstOverTime) agg(sample promql.Point) {
	if a.hasData {
		return
	}
	a.v = sample.V
	a.hasData = true
}

func (a *FirstOverTime) at() float64 {
	return a.v
}

type LastOverTime struct {
	v float64
}

func (a *LastOverTime) agg(sample promql.Point) {
	a.v = sample.V
}
func (a *LastOverTime) at() float64 {
	return a.v
}

type OneOverTime struct {
}

func (a *OneOverTime) agg(sample promql.Point) {
}
func (a *OneOverTime) at() float64 {
	return 1.0
}
