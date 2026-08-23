package logql

import (
	"math"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
)

func newCountDistinctStepEvaluator(
	it iter.PeekingSampleIterator,
	params Params,
	interval, offset time.Duration,
) (StepEvaluator, error) {
	return &RangeVectorEvaluator{
		iter: newCountDistinctIterator(
			it,
			interval.Nanoseconds(),
			params.Step().Nanoseconds(),
			params.Start().UnixNano(),
			params.End().UnixNano(),
			offset.Nanoseconds(),
		),
	}, nil
}

func newCountDistinctIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
) RangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	return &countDistinctBatchRangeVectorIterator{
		batchRangeVectorIterator: &batchRangeVectorIterator{
			iter:     it,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			window:   map[string]*promql.Series{},
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		},
	}
}

type countDistinctBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
}

func (r *countDistinctBatchRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		r.at = append(r.at, promql.Sample{
			F:      countDistinctEstimate(series.Floats),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

func countDistinctEstimate(samples []promql.FPoint) float64 {
	hll := hyperloglog.New14()
	for _, sample := range samples {
		hll.InsertHash(math.Float64bits(sample.F))
	}
	return float64(hll.Estimate())
}
