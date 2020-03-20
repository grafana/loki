package logql

import (
	"context"
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/iter"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql"
)

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *astmapper.ShardAnnotation
	SampleExpr
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *astmapper.ShardAnnotation
	LogSelectorExpr
}

// ConcatSampleExpr is an expr for concatenating multiple SampleExpr
type ConcatSampleExpr struct {
	SampleExpr
	next *ConcatSampleExpr
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	LogSelectorExpr
	next *ConcatLogSelectorExpr
}

// downstreamEvaluator is an evaluator which handles shard aware AST nodes
// and embeds a default evaluator otherwise
type downstreamEvaluator struct {
	shards int
}

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *downstreamEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv Evaluator,
	expr SampleExpr,
	params Params,
) (StepEvaluator, error) {
	switch expr.(type) {
	case DownstreamSampleExpr:
		// determine type (SampleExpr, LogSelectorExpr) and downstream to a querier
		return nil, errors.New("unimplemented")
	case ConcatSampleExpr:
		// ensure they all impl the same (SampleExpr, LogSelectorExpr) & concat
		return nil, errors.New("unimplemented")
	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
	}
}

// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *downstreamEvaluator) Iterator(
	_ context.Context,
	expr LogSelectorExpr,
	_ Params,
) (iter.EntryIterator, error) {
	return nil, fmt.Errorf("downstreamEvaluator does not implement Iterator, called with expr: %+v", expr)
}

// ConcatEvaluator joins multiple StepEvaluators.
// Contract: They must be of identical start, end, and step values.
func ConcatEvaluator(evaluators []StepEvaluator) (StepEvaluator, error) {
	return newStepEvaluator(
		func() (done bool, ts int64, vec promql.Vector) {
			var cur promql.Vector
			for _, eval := range evaluators {
				done, ts, cur = eval.Next()
				vec = append(vec, cur...)
			}
			return done, ts, vec

		},
		func() (lastErr error) {
			for _, eval := range evaluators {
				if err := eval.Close(); err != nil {
					lastErr = err
				}
			}
			return lastErr
		},
	)
}
