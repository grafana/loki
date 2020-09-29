package logql

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/prometheus/promql"
)

const unsupportedErr = "unsupported range vector aggregation operation: %s"

func (r rangeAggregationExpr) Extractor() (SampleExtractor, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if r.left.unwrap != nil {
		return newLabelSampleExtractor(r.left.unwrap.identifier), nil
	}
	switch r.operation {
	case OpRangeTypeRate, OpRangeTypeCount:
		return ExtractCount, nil
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		return ExtractBytes, nil
	default:
		return nil, fmt.Errorf(unsupportedErr, r.operation)
	}
}

func (r rangeAggregationExpr) aggregator() (RangeVectorAggregator, error) {
	switch r.operation {
	case OpRangeTypeRate:
		return rateLogs(r.left.interval), nil
	case OpRangeTypeCount:
		return countOverTime, nil
	case OpRangeTypeBytesRate:
		return rateLogBytes(r.left.interval), nil
	case OpRangeTypeBytes, OpRangeTypeSum:
		return sumOverTime, nil
	case OpRangeTypeAvg:
		return avgOverTime, nil
	case OpRangeTypeMax:
		return maxOverTime, nil
	case OpRangeTypeMin:
		return minOverTime, nil
	case OpRangeTypeStddev:
		return stddevOverTime, nil
	case OpRangeTypeStdvar:
		return stdvarOverTime, nil
	default:
		return nil, fmt.Errorf(unsupportedErr, r.operation)
	}
}

// rateLogs calculates the per-second rate of log lines.
func rateLogs(selRange time.Duration) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		return float64(len(samples)) / selRange.Seconds()
	}
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
