package logql

import (
	"github.com/prometheus/prometheus/util/kahansum"

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
//
// The constructor can depend on expr, so use it only for expr.
func newStepAccumulatorFuncFor(expr *syntax.RangeAggregationExpr) (newStepAccumulatorFunc, bool) {
	switch expr.Operation {
	case syntax.OpRangeTypeCount:
		return newCountAccumulator, true
	case syntax.OpRangeTypeSum:
		return newSumAccumulator, true
	case syntax.OpRangeTypeRate:
		return newRateAccumulatorFunc(expr), true
	default:
		return nil, false
	}
}

// StreamFirstRangeAggregations returns the range aggregation of each part of expr that can run in
// stream-first order. The parts come in walk order and can sit at any depth.
func StreamFirstRangeAggregations(expr syntax.Expr) []*syntax.RangeAggregationExpr {
	var out []*syntax.RangeAggregationExpr
	expr.Walk(func(e syntax.Expr) bool {
		rng, ok := StreamFirstRangeAggregationExpr(e)
		if ok {
			out = append(out, rng)
		}
		return !ok
	})
	return out
}

// StreamFirstRangeAggregationExpr returns the range aggregation of expr, and true, when expr itself
// can run in stream-first order. It does not search the descendants of expr.
func StreamFirstRangeAggregationExpr(expr syntax.Expr) (*syntax.RangeAggregationExpr, bool) {
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

// sumAccumulator adds up the sample values of each step with Kahan summation.
//
// Each step adds the samples of one series in arrival order, like SumOverTime does. When the
// samples arrive in timestamp order, a step holds the same sum as the timestamp-first path.
//
// Integer values sum exactly in any order while every partial sum stays below 2^53 in magnitude.
// Other values can differ in the last digits when the samples arrive in another order.
type sumAccumulator struct {
	// sums holds the sum of each step, without its compensation.
	sums []float64

	// compensations holds the Kahan compensation of each step. It stays nil until a step gets a
	// non-zero compensation, so a series with exact sums needs no memory for it.
	//
	// A sum is exact, and has no compensation, while every partial sum of integer values stays
	// below 2^53 in magnitude. A step that holds one finite sample has none either.
	compensations []float64

	// presence counts the samples of each step. value uses it to tell a step that sums to zero
	// from a step that holds no sample.
	presence countAccumulator
}

func newSumAccumulator(steps int) stepAccumulator {
	return &sumAccumulator{
		sums:     make([]float64, steps),
		presence: countAccumulator{counts: make([]int64, steps+1)},
	}
}

func (a *sumAccumulator) add(lo, hi int, v float64) {
	// Add v to each step in turn. countAccumulator instead marks the first step and the step after
	// the last, then takes a prefix sum.
	k := lo
	for ; a.compensations == nil && k <= hi; k++ {
		var c float64
		a.sums[k], c = kahansum.Inc(v, a.sums[k], 0)

		// The first loop runs while no step has a compensation. It allocates the compensations
		// at the first non-zero one. The second loop runs once they exist, so it needs no check
		// per step.
		if c != 0 {
			a.compensations = make([]float64, len(a.sums))
			a.compensations[k] = c
		}
	}
	for ; k <= hi; k++ {
		a.sums[k], a.compensations[k] = kahansum.Inc(v, a.sums[k], a.compensations[k])
	}
	a.presence.add(lo, hi, v)
}

func (a *sumAccumulator) finish() {
	a.presence.finish()
}

// value panics when finish has not run, because the presence marks are not counts yet.
func (a *sumAccumulator) value(k int) (float64, bool) {
	if _, ok := a.presence.value(k); !ok {
		return 0, false
	}
	var c float64
	if a.compensations != nil {
		c = a.compensations[k]
	}
	return kahanResult(a.sums[k], c), true
}

// newRateAccumulatorFunc returns the constructor of rate accumulators for expr, which must be a
// rate aggregation.
func newRateAccumulatorFunc(expr *syntax.RangeAggregationExpr) newStepAccumulatorFunc {
	newTotals := newCountAccumulator
	if expr.Left.Unwrap != nil {
		newTotals = newSumAccumulator
	}
	seconds := expr.Left.Interval.Seconds()

	return func(steps int) stepAccumulator {
		return &rateAccumulator{totals: newTotals(steps), seconds: seconds}
	}
}

// rateAccumulator turns the total of each step into a per-second rate.
//
// The total is the sample count, or the sum of the sample values when the query has an unwrap. The
// value of a step is its total divided by the range vector duration in seconds, as in the
// timestamp-first rate.
//
// With an unwrap, the rate can differ from the timestamp-first rate in the last digits. This
// happens when the samples arrive out of timestamp order. It needs non-integer values, or a
// partial sum of 2^53 or more.
//
// The rate does not extrapolate to the window edges, unlike rate_counter().
type rateAccumulator struct {
	totals  stepAccumulator
	seconds float64
}

func (a *rateAccumulator) add(lo, hi int, v float64) {
	a.totals.add(lo, hi, v)
}

func (a *rateAccumulator) finish() {
	a.totals.finish()
}

// value panics when finish has not run, because the wrapped totals panic.
func (a *rateAccumulator) value(k int) (float64, bool) {
	total, ok := a.totals.value(k)
	if !ok {
		return 0, false
	}

	// A range vector duration of zero adds no sample, so the check above returns before a division
	// by zero. Divide, do not multiply by 1/seconds. The timestamp-first rate divides, and the
	// product can differ in the last bit.
	return total / a.seconds, true
}
