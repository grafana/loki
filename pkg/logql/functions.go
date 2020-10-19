package logql

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql"
)

const unsupportedErr = "unsupported range vector aggregation operation: %s"

func (r rangeAggregationExpr) Extractor() (log.SampleExtractor, error) {
	return r.extractor(nil, false)
}

func (r rangeAggregationExpr) extractor(gr *grouping, all bool) (log.SampleExtractor, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	var groups []string
	var without bool

	// fallback to parents grouping
	if gr != nil {
		groups = gr.groups
		without = gr.without
	}

	// range aggregation grouping takes priority
	if r.grouping != nil {
		groups = r.grouping.groups
		without = r.grouping.without
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
		case OpConvDuration, OpConvDurationSeconds:
			convOp = log.ConvertDuration
		default:
			convOp = log.ConvertFloat
		}

		return log.LabelExtractorWithStages(
			r.left.unwrap.identifier,
			convOp, groups, without, all, stages,
			log.ReduceAndLabelFilter(r.left.unwrap.postFilters),
		)
	}
	// otherwise we extract metrics from the log line.
	switch r.operation {
	case OpRangeTypeRate, OpRangeTypeCount:
		return log.LineExtractorWithStages(log.CountExtractor, stages, groups, without, all)
	case OpRangeTypeBytes, OpRangeTypeBytesRate:
		return log.LineExtractorWithStages(log.BytesExtractor, stages, groups, without, all)
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
