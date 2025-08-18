package executor

import (
	"context"
	"errors"
	"fmt"
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

// RangeAggregationPipeline is a pipeline that performs aggregations over a time window.
//
// 1. It reads from the input pipelines
// 2. Partitions the data by the specified columns
// 3. Applies the aggregation function on each partition
//
// Current version only supports counting for instant queries.
type RangeAggregationPipeline struct {
	state           state
	inputs          []Pipeline
	inputsExhausted bool // indicates if all inputs are exhausted

	aggregator          *aggregator
	matchingTimeWindows func(t time.Time) ([]time.Time, bool) // function to find matching time windows for a given timestamp
	evaluator           expressionEvaluator                   // used to evaluate column expressions
	opts                rangeAggregationOptions
}

func NewRangeAggregationPipeline(inputs []Pipeline, evaluator expressionEvaluator, opts rangeAggregationOptions) (*RangeAggregationPipeline, error) {
	r := &RangeAggregationPipeline{
		inputs:    inputs,
		evaluator: evaluator,
		opts:      opts,
	}
	r.init()
	return r, nil
}

func (r *RangeAggregationPipeline) init() {
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

	r.matchingTimeWindows = func(t time.Time) ([]time.Time, bool) {
		if t.Compare(lowerbound) <= 0 || t.Compare(upperbound) > 0 {
			return nil, false // out of range
		}

		var ret []time.Time
		for _, window := range windows {
			if t.Compare(window.startTs) > 0 && t.Compare(window.endTs) <= 0 {
				ret = append(ret, window.endTs)
			}
		}

		return ret, true
	}

	r.aggregator = newAggregator(r.opts.partitionBy, len(windows))
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
func (r *RangeAggregationPipeline) Read(ctx context.Context) error {
	// if the state already has an error, do not attempt to read.
	if r.state.err != nil {
		return r.state.err
	}

	if r.inputsExhausted {
		r.state = failureState(EOF)
		return r.state.err
	}

	if r.state.batch != nil {
		r.state.batch.Release()
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
func (r *RangeAggregationPipeline) read(ctx context.Context) (arrow.Record, error) {
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
				windows, ok := r.matchingTimeWindows(tsCol.Value(row).ToTime(arrow.Nanosecond))
				if !ok {
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
	return r.aggregator.buildRecord()
}

// Value returns the current value in state.
func (r *RangeAggregationPipeline) Value() (arrow.Record, error) {
	return r.state.Value()
}

// Close closes the resources of the pipeline.
// The implementation must close all the of the pipeline's inputs.
func (r *RangeAggregationPipeline) Close() {
	// Release last batch
	if r.state.batch != nil {
		r.state.batch.Release()
	}

	for _, input := range r.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (r *RangeAggregationPipeline) Inputs() []Pipeline {
	return r.inputs
}

// Transport returns the type of transport of the implementation.
func (r *RangeAggregationPipeline) Transport() Transport {
	return Local
}
