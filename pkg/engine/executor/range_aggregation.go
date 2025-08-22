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

	// Use arithmetic-based O(1) lookup for aligned windows
	if r.opts.step > 0 && r.opts.step == r.opts.rangeInterval {
		r.matchingTimeWindows = r.createArithmeticMatcher(windows, lowerbound, upperbound)
	} else {
		// Fall back to existing binary search for non-aligned windows
		r.matchingTimeWindows = func(t time.Time) []time.Time {
			if t.Compare(lowerbound) <= 0 || t.Compare(upperbound) > 0 {
				return nil // out of range
			}

			// For small number of windows, linear search is more efficient
			if len(windows) <= 10 {
				var ret []time.Time
				for _, window := range windows {
					if t.Compare(window.startTs) > 0 && t.Compare(window.endTs) <= 0 {
						ret = append(ret, window.endTs)
					}
				}
				return ret
			}

			// Use binary search for larger number of windows
			// Find the first window where t <= endTs (could contain t)
			firstIdx := sort.Search(len(windows), func(i int) bool {
				return t.Compare(windows[i].endTs) <= 0
			})

			// Find the first window where t > startTs (could contain t)
			lastIdx := sort.Search(len(windows), func(i int) bool {
				return t.Compare(windows[i].startTs) > 0
			})

			// The matching windows are in the range [lastIdx, firstIdx-1]
			// But we need to verify each window actually contains t
			var result []time.Time
			for i := lastIdx; i < firstIdx; i++ {
				window := windows[i]
				if t.Compare(window.startTs) > 0 && t.Compare(window.endTs) <= 0 {
					result = append(result, window.endTs)
				}
			}

			return result
		}
	}

	r.aggregator = newAggregator(r.opts.partitionBy, len(windows))
}

// O(1) arithmetic lookup for aligned windows (step == rangeInterval)
func (r *rangeAggregationPipeline) createArithmeticMatcher(
	windows []struct{ startTs, endTs time.Time },
	lowerbound, upperbound time.Time,
) func(time.Time) []time.Time {
	startNs := r.opts.startTs.UnixNano()
	stepNs := r.opts.step.Nanoseconds()

	return func(t time.Time) []time.Time {
		tNs := t.UnixNano()

		// For aligned windows, window i covers: (startTs + i*step - step, startTs + i*step]
		// This means: t > startTs + i*step - step && t <= startTs + i*step
		// So: t > startTs + (i-1)*step && t <= startTs + i*step

		// Calculate window index
		// We want: t > startTs + (i-1)*step && t <= startTs + i*step
		// This means: t > startTs + i*step - step && t <= startTs + i*step
		// So: i*step >= t - startTs && i*step > t - startTs - step
		// Therefore: i = ceil((t - startTs) / step)

		windowIndex := (tNs - startNs + stepNs - 1) / stepNs

		// Check bounds - this handles all out-of-range cases
		if windowIndex < 0 || windowIndex >= int64(len(windows)) {
			return nil
		}

		// Verify the timestamp is actually within this window
		// This is required because the arithmetic formula doesn't perfectly handle
		// the exclusive/inclusive boundary conditions
		window := windows[windowIndex]
		if t.Compare(window.startTs) > 0 && t.Compare(window.endTs) <= 0 {
			return []time.Time{window.endTs}
		}

		return nil
	}
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
