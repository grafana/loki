package executor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type rangeAggregationOptions struct {
	partitionBy []physical.ColumnExpression

	// start and end timestamps are equal for instant queries.
	startTs       time.Time     // start timestamp of the query
	endTs         time.Time     // end timestamp of the query
	rangeInterval time.Duration // range interval
	step          time.Duration // step used for range queries
}

// rangeAggregationPipeline is a pipeline that performs aggregations over a time window.
//
// 1. It reads from the input pipelines
// 2. Partitions the data by the specified columns
// 3. Applies the aggregation function on each partition
//
// Current version only supports counting for instant queries.
type rangeAggregationPipeline struct {
	state           state
	inputs          []Pipeline
	inputsExhausted bool // indicates if all inputs are exhausted

	aggregator          *aggregator
	matchingTimeWindows func(t time.Time) []time.Time // function to find matching time windows for a given timestamp
	evaluator           expressionEvaluator           // used to evaluate column expressions
	opts                rangeAggregationOptions
}

func newRangeAggregationPipeline(inputs []Pipeline, evaluator expressionEvaluator, opts rangeAggregationOptions) (*rangeAggregationPipeline, error) {
	r := &rangeAggregationPipeline{
		inputs:    inputs,
		evaluator: evaluator,
		opts:      opts,
	}
	r.init()
	return r, nil
}

func (r *rangeAggregationPipeline) init() {
	windows := []struct {
		// lower bound is not inclusive
		// refer to [logql.batchRangeVectorIterator]
		startTs time.Time
		endTs   time.Time
	}{}
	cur := r.opts.startTs
	for cur.Compare(r.opts.endTs) <= 0 {
		windows = append(windows, struct {
			startTs time.Time
			endTs   time.Time
		}{
			startTs: cur.Add(-r.opts.rangeInterval),
			endTs:   cur,
		})

		if r.opts.step == 0 {
			break
		}

		// advance to the next window using step
		cur = cur.Add(r.opts.step)
	}

	var (
		lowerbound = r.opts.startTs.Add(-r.opts.rangeInterval)
		upperbound = r.opts.endTs
	)

	switch true {
	case r.opts.step == 0: // for instant queries
		r.matchingTimeWindows = func(t time.Time) []time.Time {
			if t.Compare(lowerbound) <= 0 || t.Compare(upperbound) > 0 {
				return nil // out of range
			}

			return []time.Time{windows[0].endTs}
		}
	case r.opts.step == r.opts.rangeInterval:
		r.matchingTimeWindows = r.createAlignedMatcher(windows, lowerbound, upperbound)
	case r.opts.step > r.opts.rangeInterval:
		r.matchingTimeWindows = r.createGappedMatcher(windows, lowerbound, upperbound)
	case r.opts.step < r.opts.rangeInterval:
		r.matchingTimeWindows = r.createOverlappingMatcher(windows, lowerbound, upperbound)
	default:
		panic("invalid step and range interval")
	}

	r.aggregator = newAggregator(r.opts.partitionBy, len(windows))
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
func (r *rangeAggregationPipeline) Read(ctx context.Context) error {
	// if the state already has an error, do not attempt to read.
	if r.state.err != nil {
		return r.state.err
	}

	if r.inputsExhausted {
		r.state = failureState(EOF)
		return r.state.err
	}

	record, err := r.read(ctx)
	r.state = newState(record, err)

	if err != nil {
		return fmt.Errorf("run range aggregation: %w", err)
	}
	return nil
}

// TODOs:
// - Support implicit partitioning by all labels when partitionBy is empty
// - Use columnar access pattern. Current approach is row-based which does not benefit from the storage format.
// - Add toggle to return partial results on Read() call instead of returning only after exhausing all inputs.
func (r *rangeAggregationPipeline) read(ctx context.Context) (arrow.Record, error) {
	var (
		tsColumnExpr = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		} // timestamp column expression

		// reused on each row read
		labelValues = make([]string, len(r.opts.partitionBy))
	)

	r.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range r.inputs {
			if err := input.Read(ctx); err != nil {
				if errors.Is(err, EOF) {
					continue
				}

				return nil, err
			}

			inputsExhausted = false
			record, _ := input.Value()
			defer record.Release()

			// extract all the columns that are used for partitioning
			arrays := make([]*array.String, 0, len(r.opts.partitionBy))
			for _, columnExpr := range r.opts.partitionBy {
				vec, err := r.evaluator.eval(columnExpr, record)
				if err != nil {
					return nil, err
				}

				if vec.Type() != datatype.Loki.String {
					return nil, fmt.Errorf("unsupported datatype for partitioning %s", vec.Type())
				}

				arrays = append(arrays, vec.ToArray().(*array.String))
			}

			// extract timestamp column to check if the entry is in range
			vec, err := r.evaluator.eval(tsColumnExpr, record)
			if err != nil {
				return nil, err
			}
			tsCol := vec.ToArray().(*array.Timestamp)

			for row := range int(record.NumRows()) {
				windows := r.matchingTimeWindows(tsCol.Value(row).ToTime(arrow.Nanosecond))
				if len(windows) == 0 {
					continue // out of range, skip this row
				}

				// reset label values and hash for each row
				clear(labelValues)
				for col, arr := range arrays {
					labelValues[col] = arr.Value(row)
				}

				for _, ts := range windows {
					r.aggregator.Add(ts, 1, labelValues)
				}
			}
		}
	}

	r.inputsExhausted = true
	return r.aggregator.BuildRecord()
}

// Value returns the current value in state.
func (r *rangeAggregationPipeline) Value() (arrow.Record, error) {
	return r.state.Value()
}

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *rangeAggregationPipeline) Close() {
	for _, input := range r.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (r *rangeAggregationPipeline) Inputs() []Pipeline {
	return r.inputs
}

// Transport returns the type of transport of the implementation.
func (r *rangeAggregationPipeline) Transport() Transport {
	return Local
}

func (r *rangeAggregationPipeline) createAlignedMatcher(
	windows []struct{ startTs, endTs time.Time },
	lowerbound, upperbound time.Time,
) func(time.Time) []time.Time {
	startNs := r.opts.startTs.UnixNano()
	stepNs := r.opts.step.Nanoseconds()

	return func(t time.Time) []time.Time {
		tNs := t.UnixNano()

		// out of bounds check
		if tNs <= lowerbound.UnixNano() || tNs > upperbound.UnixNano() {
			return nil
		}

		// valid timestamps for window i: t > startNs + (i-1) * stepNs && t <= startNs + i * stepNs
		// i = ceil((t - startTs) / step)
		windowIndex := (tNs - startNs + stepNs - 1) / stepNs

		// endTs can be calculated, but doing a window look-up instead for
		// convinience as it is pre-calculated for other scenarios.
		return []time.Time{windows[windowIndex].endTs}
	}
}

func (r *rangeAggregationPipeline) createGappedMatcher(
	windows []struct{ startTs, endTs time.Time },
	lowerbound, upperbound time.Time,
) func(time.Time) []time.Time {
	startNs := r.opts.startTs.UnixNano()
	stepNs := r.opts.step.Nanoseconds()

	return func(t time.Time) []time.Time {
		tNs := t.UnixNano()

		// out of bounds check
		if tNs <= lowerbound.UnixNano() || tNs > upperbound.UnixNano() {
			return nil
		}

		// For gapped windows, window i covers: (startTs + i*step - range, startTs + i*step]
		//
		// 1. Find the index similar to aligned windows: ceil((t - startTs) / step)
		// 2. Additionally check if t is within the window (not in a gap)

		windowIndex := (tNs - startNs + stepNs - 1) / stepNs
		window := windows[windowIndex]

		// Verify the timestamp is within the window (not in a gap)
		if tNs > window.startTs.UnixNano() {
			return []time.Time{window.endTs}
		}

		return nil // timestamp is in a gap
	}
}

func (r *rangeAggregationPipeline) createOverlappingMatcher(
	windows []struct{ startTs, endTs time.Time },
	lowerbound, upperbound time.Time,
) func(time.Time) []time.Time {
	return func(t time.Time) []time.Time {
		// out of bounds check
		if t.Compare(lowerbound) <= 0 || t.Compare(upperbound) > 0 {
			return nil
		}

		// Find the last window that could contain the timestamp.
		// We need to find the last window where t > window.startTs
		// so search for the first window where t <= window.startTs
		firstOOBIndex := sort.Search(len(windows), func(i int) bool {
			return t.Compare(windows[i].startTs) <= 0
		})

		windowIndex := firstOOBIndex - 1
		if windowIndex < 0 {
			return nil
		}

		// Iterate backwards from last matching window to find all matches
		var result []time.Time
		for i := windowIndex; i >= 0; i-- {
			window := windows[i]
			if t.Compare(window.startTs) > 0 && t.Compare(window.endTs) <= 0 {
				result = append(result, window.endTs)
			} else if t.Compare(window.endTs) > 0 {
				// we've gone past all possible matches
				break
			}
		}

		return result
	}
}
