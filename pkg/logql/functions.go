package logql

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql"
)

const unsupportedErr = "unsupported range vector aggregation operation: %s"

func (r rangeAggregationExpr) Extractor() (SampleExtractor, error) {
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
	case OpRangeTypeBytes:
		return sumOverTime, nil
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
