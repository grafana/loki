package logql

import (
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// stepAccumulator accumulates the samples of one series in each query step.
type stepAccumulator interface {
	// add adds a sample with value v to the steps lo to hi, both inclusive.
	add(lo, hi int, v float64)

	// finish makes the step values readable. Call it once, after the last add and before value.
	finish()

	// value returns the value of step k, and false when no sample fell in the window of the step.
	value(k int) (float64, bool)
}

// newStepAccumulatorFunc returns an empty stepAccumulator for the given number of steps.
type newStepAccumulatorFunc func(steps int) stepAccumulator

// newStepAccumulatorFuncFor returns the accumulator constructor of a range aggregation, and false
// if stream-first order does not support the aggregation.
func newStepAccumulatorFuncFor(expr *syntax.RangeAggregationExpr) (newStepAccumulatorFunc, bool) {
	switch expr.Operation {
	case syntax.OpRangeTypeCount:
		return newCountAccumulator, true
	default:
		return nil, false
	}
}

// StreamFirstRangeAggregation returns the range aggregation of query, and true, when the engine
// is capable of running query in stream-first sample order.
func StreamFirstRangeAggregation(query string) (*syntax.RangeAggregationExpr, bool) {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return nil, false
	}
	vec, ok := expr.(*syntax.VectorAggregationExpr)
	if !ok || vec.Operation != syntax.OpTypeSum {
		return nil, false
	}
	rng, ok := vec.Left.(*syntax.RangeAggregationExpr)
	if !ok {
		return nil, false
	}
	if _, ok := newStepAccumulatorFuncFor(rng); !ok {
		return nil, false
	}
	return rng, true
}

// countAccumulator counts the samples of each step.
type countAccumulator struct {
	// counts holds the +1/-1 marks before finish, with one extra slot after the last step. After
	// finish, counts[k] is the sample count of step k.
	counts   []int64
	finished bool
}

func newCountAccumulator(steps int) stepAccumulator {
	return &countAccumulator{counts: make([]int64, steps+1)}
}

func (a *countAccumulator) add(lo, hi int, _ float64) {
	// A sample falls in up to ceil(selRange/step) windows. Instead of incrementing each window,
	// mark +1 at the first step and -1 after the last step, so each sample costs O(1).
	a.counts[lo]++
	a.counts[hi+1]--
}

func (a *countAccumulator) finish() {
	// The prefix sum turns the marks of add into counts. It costs O(steps) once per series.
	for k := 1; k < len(a.counts); k++ {
		a.counts[k] += a.counts[k-1]
	}
	a.finished = true
}

// value panics when finish has not run, because the marks are not counts yet.
func (a *countAccumulator) value(k int) (float64, bool) {
	if !a.finished {
		panic("countAccumulator: value called before finish")
	}
	return float64(a.counts[k]), a.counts[k] > 0
}
