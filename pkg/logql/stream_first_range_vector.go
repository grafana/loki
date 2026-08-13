package logql

import (
	"fmt"
	"math"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// getSampleOrderForExpr decides which sample order to request for a range aggregation. It returns
// SAMPLE_ORDER_BY_STREAM to route the query through the stream-first evaluator when stream-ordered
// execution is enabled and the aggregation is decomposable, and SAMPLE_ORDER_BY_TIMESTAMP (the
// default per-timestamp path) otherwise.
//
// Only decomposable range aggregations can be evaluated by the stream-first iterator: their
// per-window result can be accumulated incrementally, one sample at a time, without keeping the raw
// samples. Non-decomposable operations (rate_counter, stddev, stdvar, quantile, first, last,
// absent) stay on the default path.
func getSampleOrderForExpr(streamOrderedExecutionEnabled bool, expr *syntax.RangeAggregationExpr) logproto.SampleOrder {
	if !streamOrderedExecutionEnabled {
		return logproto.SAMPLE_ORDER_BY_TIMESTAMP
	}
	switch expr.Operation {
	case syntax.OpRangeTypeCount,
		syntax.OpRangeTypeSum,
		syntax.OpRangeTypeBytes,
		syntax.OpRangeTypeBytesRate,
		syntax.OpRangeTypeRate,
		syntax.OpRangeTypeAvg,
		syntax.OpRangeTypeMin,
		syntax.OpRangeTypeMax:
		return logproto.SAMPLE_ORDER_BY_STREAM
	default:
		return logproto.SAMPLE_ORDER_BY_TIMESTAMP
	}
}

// streamFirstRangeVectorIterator is a RangeVectorIterator that evaluates a decomposable range
// aggregation by draining the (order-independent) sample iterator once into per-(series, step)
// reductions, then replaying the result step by step.
type streamFirstRangeVectorIterator struct {
	it       iter.PeekingSampleIterator
	reducer  reducer
	selRange int64
	step     int64
	start    int64
	end      int64
	offset   int64
	lastStep int
	numSteps int

	loaded  bool
	series  map[string]*seriesState
	metrics map[string]labels.Labels

	curStep int
	at      []promql.Sample
	err     error
}

// newStreamFirstRangeVectorIterator builds a stream-first iterator. start/end are expected to be
// already offset-adjusted and step already normalized by newRangeVectorIterator.
func newStreamFirstRangeVectorIterator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64,
) (RangeVectorIterator, error) {
	red, ok := reducerFor(expr)
	if !ok {
		return nil, fmt.Errorf(syntax.UnsupportedErr, expr.Operation)
	}
	lastStep := int((end - start) / step)
	return &streamFirstRangeVectorIterator{
		it:       it,
		reducer:  red,
		selRange: selRange,
		step:     step,
		start:    start,
		end:      end,
		offset:   offset,
		lastStep: lastStep,
		numSteps: lastStep + 1,
		series:   map[string]*seriesState{},
		metrics:  map[string]labels.Labels{},
		curStep:  -1,
	}, nil
}

func (r *streamFirstRangeVectorIterator) Next() bool {
	if !r.loaded {
		r.load()
		r.loaded = true
	}
	if r.err != nil {
		return false
	}
	r.curStep++
	return r.curStep <= r.lastStep
}

func (r *streamFirstRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.series))
	}
	r.at = r.at[:0]

	current := r.start + int64(r.curStep)*r.step
	// convert ts from nanoseconds to milliseconds, as the iterator works in nanoseconds.
	ts := current/1e+6 + r.offset/1e+6

	for _, series := range r.series {
		step := series.steps[r.curStep]
		if step.count == 0 { // empty window
			continue
		}
		r.at = append(r.at, promql.Sample{F: r.reducer.finalize(step), T: ts, Metric: series.metric})
	}
	return ts, SampleVector(r.at)
}

func (r *streamFirstRangeVectorIterator) Close() error {
	return r.it.Close()
}

func (r *streamFirstRangeVectorIterator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.it.Err()
}

// load drains the sample iterator once, folding each sample into the reduction of every window it
// covers. It stops and records an error if a sample's series labels cannot be parsed.
func (r *streamFirstRangeVectorIterator) load() {
	var (
		currSeriesLabels string
		currSeriesState  *seriesState
	)

	for lbs, sample, ok := r.it.Peek(); ok; lbs, sample, ok = r.it.Peek() {
		lo, hi, inRange := r.windowRange(sample.Timestamp)
		if !inRange {
			_ = r.it.Next()
			continue
		}

		// Samples are iterated in stream order, so there's a high chance that the
		// next sample belongs to the current series. In such a case, we avoid a map
		// lookup.
		if currSeriesState == nil || lbs != currSeriesLabels {
			series, err := r.seriesFor(lbs)
			if err != nil {
				r.err = err
				return
			}
			currSeriesLabels, currSeriesState = lbs, series
		}

		// Process the sample in each matching step.
		for k := lo; k <= hi; k++ {
			r.reducer.reduce(&currSeriesState.steps[k], sample.Value)
		}

		_ = r.it.Next()
	}
}

// windowRange returns the inclusive range of step indices [lo, hi] whose window contains ts.
// Step k covers the window (s_k-selRange, s_k] with s_k = start + k*step, so ts belongs to
// steps with s_k in [ts, ts+selRange), clamped to [0, lastStep]. Returns ok=false if none.
func (r *streamFirstRangeVectorIterator) windowRange(ts int64) (lo, hi int, ok bool) {
	kLo := ceilDivInt64(ts-r.start, r.step)
	if kLo < 0 {
		kLo = 0
	}
	kHi := floorDivInt64(ts+r.selRange-1-r.start, r.step)
	if kHi > int64(r.lastStep) {
		kHi = int64(r.lastStep)
	}
	if kLo > kHi {
		return 0, 0, false
	}
	return int(kLo), int(kHi), true
}

func (r *streamFirstRangeVectorIterator) seriesFor(lbs string) (*seriesState, error) {
	if s := r.series[lbs]; s != nil {
		return s, nil
	}
	metric, ok := r.metrics[lbs]
	if !ok {
		var err error
		metric, err = promql_parser.NewParser(promql_parser.Options{}).ParseMetric(lbs)
		if err != nil {
			return nil, fmt.Errorf("parsing series labels %q: %w", lbs, err)
		}
		r.metrics[lbs] = metric
	}
	s := &seriesState{metric: metric, steps: make([]reduction, r.numSteps)}
	r.series[lbs] = s
	return s, nil
}

// ceilDivInt64 returns ceil(a/b) for b > 0, handling negative a correctly.
func ceilDivInt64(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a > 0) == (b > 0) {
		q++
	}
	return q
}

// floorDivInt64 returns floor(a/b) for b > 0, handling negative a correctly.
func floorDivInt64(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a > 0) != (b > 0) {
		q--
	}
	return q
}

// seriesState holds one output series' per-step reductions, one per query step.
type seriesState struct {
	metric labels.Labels
	steps  []reduction
}

// reduction is the running (value, count) reduction of one (series, step).
type reduction struct {
	value float64
	count int64
}

// reducer reduces samples into a reduction and finalizes it.
type reducer struct {
	reduce   func(r *reduction, v float64)
	finalize func(r reduction) float64
}

// reducerFor returns the reducer for a decomposable range aggregation.
func reducerFor(expr *syntax.RangeAggregationExpr) (reducer, bool) {
	selRangeSeconds := expr.Left.Interval.Seconds()

	switch expr.Operation {
	case syntax.OpRangeTypeCount:
		return reducer{
			reduce:   func(r *reduction, _ float64) { r.count++ },
			finalize: func(r reduction) float64 { return float64(r.count) },
		}, true

	case syntax.OpRangeTypeSum, syntax.OpRangeTypeBytes:
		return reducer{
			reduce:   func(r *reduction, v float64) { r.value += v; r.count++ },
			finalize: func(r reduction) float64 { return r.value },
		}, true

	case syntax.OpRangeTypeBytesRate:
		return reducer{
			reduce:   func(r *reduction, v float64) { r.value += v; r.count++ },
			finalize: func(r reduction) float64 { return r.value / selRangeSeconds },
		}, true

	case syntax.OpRangeTypeRate:
		if expr.Left.Unwrap != nil {
			// value-based rate: sum of extracted values over the range, per second.
			return reducer{
				reduce:   func(r *reduction, v float64) { r.value += v; r.count++ },
				finalize: func(r reduction) float64 { return r.value / selRangeSeconds },
			}, true
		}
		// count-based rate: number of log lines over the range, per second.
		return reducer{
			reduce:   func(r *reduction, _ float64) { r.count++ },
			finalize: func(r reduction) float64 { return float64(r.count) / selRangeSeconds },
		}, true

	case syntax.OpRangeTypeAvg:
		// Streaming mean, matching avgOverTime exactly (including its Inf handling).
		return reducer{
			reduce: func(r *reduction, v float64) {
				r.count++
				n := float64(r.count)
				if math.IsInf(r.value, 0) {
					if math.IsInf(v, 0) && (r.value > 0) == (v > 0) {
						return
					}
					if !math.IsInf(v, 0) && !math.IsNaN(v) {
						return
					}
				}
				r.value += v/n - r.value/n
			},
			finalize: func(r reduction) float64 { return r.value },
		}, true

	case syntax.OpRangeTypeMax:
		return reducer{
			reduce: func(r *reduction, v float64) {
				if r.count == 0 || v > r.value || math.IsNaN(r.value) {
					r.value = v
				}
				r.count++
			},
			finalize: func(r reduction) float64 { return r.value },
		}, true

	case syntax.OpRangeTypeMin:
		return reducer{
			reduce: func(r *reduction, v float64) {
				if r.count == 0 || v < r.value || math.IsNaN(r.value) {
					r.value = v
				}
				r.count++
			},
			finalize: func(r reduction) float64 { return r.value },
		}, true

	default:
		return reducer{}, false
	}
}
