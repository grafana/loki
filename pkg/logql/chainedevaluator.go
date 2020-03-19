package logql

import (
	"context"

	"github.com/grafana/loki/pkg/iter"
	"github.com/pkg/errors"
)

// ChainedEvaluator is an evaluator which chains multiple other evaluators,
// deferring to the first successful one.
type ChainedEvaluator struct {
	evaluators []Evaluator
}

// StepEvaluator attempts the embedded evaluators until one succeeds or they all error.
func (c *ChainedEvaluator) StepEvaluator(
	ctx context.Context,
	nextEvaluator Evaluator,
	expr SampleExpr,
	p Params,
) (stepper StepEvaluator, err error) {
	for _, eval := range c.evaluators {
		if stepper, err = eval.StepEvaluator(ctx, nextEvaluator, expr, p); err == nil {
			return stepper, nil
		}
	}
	return nil, err
}

// Iterator attempts the embedded evaluators until one succeeds or they all error.
func (c *ChainedEvaluator) Iterator(
	ctx context.Context,
	expr LogSelectorExpr,
	p Params,
) (iterator iter.EntryIterator, err error) {
	for _, eval := range c.evaluators {
		if iterator, err = eval.Iterator(ctx, expr, p); err == nil {
			return iterator, nil
		}
	}
	return nil, err
}

// NewChainedEvaluator constructs a ChainedEvaluator from one or more Evaluators
func NewChainedEvaluator(evals ...Evaluator) (*ChainedEvaluator, error) {
	if len(evals) == 0 {
		return nil, errors.New("must supply an Evaluator")
	}
	return &ChainedEvaluator{
		evaluators: evals,
	}, nil
}
