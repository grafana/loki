package logql

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type ProbabilisticEvaluator struct {
	DefaultEvaluator
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewProbabilisticEvaluator(querier Querier, maxLookBackPeriod time.Duration) Evaluator {
	d := NewDefaultEvaluator(querier, maxLookBackPeriod)
	// TODO(karsten): override or inject vectorAggEvaluator and rangeVectorEvaluator.
	// rangeVectorEvaluator.
	return &ProbabilisticEvaluator{DefaultEvaluator: *d}
}

func probabilisticVectorAggEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *syntax.VectorAggregationExpr,
	q Params,
) (StepEvaluator, error) {
	return nil, nil
}
