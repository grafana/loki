package logql

import (
	"context"
	"fmt"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser/posrange"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// newHistogramFuncEvaluator returns the evaluator of a function that reads a histogram, such as
// histogram_quantile. Its operand returns histograms, and it returns floats.
func newHistogramFuncEvaluator(
	ctx context.Context,
	evFactory SampleEvaluatorFactory,
	expr *syntax.HistogramFuncExpr,
	q Params,
) (*HistogramFuncEvaluator, error) {
	// The context is the conservative one, as label_replace has it: what the function returns is not
	// known to keep every series that the operand has.
	nextEvaluator, err := evFactory.NewStepEvaluator(ctx, evFactory, expr.Left, q, NodeContext{})
	if err != nil {
		return nil, err
	}
	return &HistogramFuncEvaluator{nextEvaluator: nextEvaluator, expr: expr}, nil
}

// HistogramFuncEvaluator evaluates histogram_quantile, histogram_quantiles, histogram_fraction,
// histogram_count, histogram_sum and histogram_avg. They use the functions that PromQL uses, so a
// quantile or a fraction is interpolated in the same way, and returns the same value, as in Prometheus.
//
// The series of the result have the labels of the series of the histograms, and histogram_quantiles adds
// the label of the quantile. The annotations that PromQL returns, for example when the histogram has no
// observation, are not used.
type HistogramFuncEvaluator struct {
	nextEvaluator StepEvaluator
	expr          *syntax.HistogramFuncExpr

	// err is the error of the function itself. The error of the operand is returned by the operand.
	err error
}

func (e *HistogramFuncEvaluator) Next() (bool, int64, StepResult) {
	if e.err != nil {
		return false, 0, SampleVector{}
	}
	next, ts, r := e.nextEvaluator.Next()
	if !next {
		return false, 0, SampleVector{}
	}

	vec := r.SampleVector()
	size := len(vec)
	if e.expr.Operation == syntax.OpHistogramQuantiles {
		size *= len(e.expr.Params)
	}
	out := make(promql.Vector, 0, size)
	for _, s := range vec {
		if s.H == nil {
			// The parser only allows a histogram as the operand, so this is not a query that can be written.
			// It is a client error, as the query is what is wrong, and it is the same on every retry.
			e.err = fmt.Errorf("%w: %s needs a histogram", logqlerr.ErrParse, e.expr.Operation)
			return false, 0, SampleVector{}
		}
		out = e.appendSamples(out, ts, s)
	}
	return next, ts, SampleVector(out)
}

// appendSamples appends the samples that the function returns for the histogram of s.
func (e *HistogramFuncEvaluator) appendSamples(out promql.Vector, ts int64, s promql.Sample) promql.Vector {
	h := s.H
	switch e.expr.Operation {
	case syntax.OpHistogramQuantile:
		v, _ := promql.HistogramQuantile(e.expr.Params[0], h, "", posrange.PositionRange{})
		return append(out, promql.Sample{T: ts, Metric: s.Metric, F: v})

	case syntax.OpHistogramQuantiles:
		for _, q := range e.expr.Params {
			v, _ := promql.HistogramQuantile(q, h, "", posrange.PositionRange{})
			// A label of the series with the name of the quantile label is replaced.
			metric := labels.NewBuilder(s.Metric).Set(e.expr.Label, labels.FormatOpenMetricsFloat(q)).Labels()
			out = append(out, promql.Sample{T: ts, Metric: metric, F: v})
		}
		return out

	case syntax.OpHistogramFraction:
		v, _ := promql.HistogramFraction(e.expr.Params[0], e.expr.Params[1], h, "", posrange.PositionRange{})
		return append(out, promql.Sample{T: ts, Metric: s.Metric, F: v})

	case syntax.OpHistogramCount:
		return append(out, promql.Sample{T: ts, Metric: s.Metric, F: h.Count})

	case syntax.OpHistogramSum:
		return append(out, promql.Sample{T: ts, Metric: s.Metric, F: h.Sum})

	case syntax.OpHistogramAvg:
		// 0 / 0 is NaN, which is the average of no observation.
		return append(out, promql.Sample{T: ts, Metric: s.Metric, F: h.Sum / h.Count})

	default:
		// The parser only builds the functions above.
		panic(fmt.Errorf("unexpected histogram function %q", e.expr.Operation))
	}
}

func (e *HistogramFuncEvaluator) Close() error {
	return e.nextEvaluator.Close()
}

func (e *HistogramFuncEvaluator) Error() error {
	if e.err != nil {
		return e.err
	}
	return e.nextEvaluator.Error()
}
