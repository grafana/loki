package logql

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logql/log"
)

const unsupportedErr = "unsupported range vector aggregation operation: %s"

func (r rangeAggregationExpr) Extractor() (log.SampleExtractor, error) {
	return r.extractor(nil)
}

// extractor creates a SampleExtractor but allows for the grouping to be overridden.
func (r rangeAggregationExpr) extractor(override *grouping) (log.SampleExtractor, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	var groups []string
	var without bool
	var noLabels bool

	if r.grouping != nil {
		groups = r.grouping.groups
		without = r.grouping.without
		if len(groups) == 0 {
			noLabels = true
		}
	}

	// uses override if it exists
	if override != nil {
		groups = override.groups
		without = override.without
		if len(groups) == 0 {
			noLabels = true
		}
	}

	sort.Strings(groups)

	var stages []log.Stage
	if p, ok := r.left.left.(*pipelineExpr); ok {
		// if the expression is a pipeline then take all stages into account first.
		st, err := p.pipeline.stages()
		if err != nil {
			return nil, err
		}
		stages = st
	}
	// unwrap...means we want to extract metrics from labels.
	if r.left.unwrap != nil {
		var convOp string
		switch r.left.unwrap.operation {
		case OpConvBytes:
			convOp = log.ConvertBytes
		case OpConvDuration, OpConvDurationSeconds:
			convOp = log.ConvertDuration
		default:
			convOp = log.ConvertFloat
		}

		return log.LabelExtractorWithStages(
			r.left.unwrap.identifier,
			convOp, groups, without, noLabels, stages,
			log.ReduceAndLabelFilter(r.left.unwrap.postFilters),
		)
	}
	// otherwise we extract metrics from the log line.
	switch r.operation {
	case OpRangeTypeRate, OpRangeTypeCount, OpRangeTypeAbsent:
		return log.NewLineSampleExtractor(log.CountExtractor, stages, groups, without, noLabels)
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		return log.NewLineSampleExtractor(log.BytesExtractor, stages, groups, without, noLabels)
	default:
		return nil, fmt.Errorf(unsupportedErr, r.operation)
	}
}

func (r rangeAggregationExpr) aggregator() (RangeVectorAggregator, error) {
	switch r.operation {
	case OpRangeTypeRate:
		return rateLogs(r.left.interval, r.left.unwrap != nil), nil
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
	case OpRangeTypeQuantile:
		return quantileOverTime(*r.params), nil
	case OpRangeTypeAbsent:
		return one, nil
	default:
		return nil, fmt.Errorf(unsupportedErr, r.operation)
	}
}

// rateLogs calculates the per-second rate of log lines.
func rateLogs(selRange time.Duration, computeValues bool) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		if !computeValues {
			return float64(len(samples)) / selRange.Seconds()
		}
		var total float64
		for _, p := range samples {
			total += p.V
		}
		return total / selRange.Seconds()
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

func quantileOverTime(q float64) func(samples []promql.Point) float64 {
	return func(samples []promql.Point) float64 {
		values := make(vectorByValueHeap, 0, len(samples))
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
func quantile(q float64, values vectorByValueHeap) float64 {
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

func one(samples []promql.Point) float64 {
	return 1.0
}
