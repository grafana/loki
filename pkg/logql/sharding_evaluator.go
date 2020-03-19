package logql

import (
	"context"
	"fmt"

	"github.com/grafana/loki/pkg/iter"
	"github.com/pkg/errors"
)

// downstreamEvaluator is an evaluator which handles shard aware AST nodes
// and embeds a default evaluator otherwise
type downstreamEvaluator struct {
	shards int
}

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *downstreamEvaluator) Evaluator(
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
		// used for aggregating downstreamed exprs, literalExprs
		return nextEv.Evaluator(ctx, nextEv, expr, params)
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
