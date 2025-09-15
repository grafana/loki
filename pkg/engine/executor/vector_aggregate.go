package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// vectorAggregationPipeline is a pipeline that performs vector aggregations.
//
// It reads from the input pipeline, groups the data by specified columns,
// and applies the aggregation function on each group.
type vectorAggregationPipeline struct {
	inputs          []Pipeline
	inputsExhausted bool // indicates if all inputs are exhausted

	aggregator *aggregator
	evaluator  expressionEvaluator
	groupBy    []physical.ColumnExpression

	tsEval    evalFunc // used to evaluate the timestamp column
	valueEval evalFunc // used to evaluate the value column
}

func newVectorAggregationPipeline(inputs []Pipeline, groupBy []physical.ColumnExpression, evaluator expressionEvaluator) (*vectorAggregationPipeline, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("vector aggregation expects at least one input")
	}

	return &vectorAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		groupBy:    groupBy,
		aggregator: newAggregator(groupBy, 0),
		tsEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}),
		valueEval: evaluator.newFunc(&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}),
	}, nil
}

// Read reads the next value into its state.
func (v *vectorAggregationPipeline) Read(ctx context.Context) (arrow.Record, error) {
	if v.inputsExhausted {
		return nil, EOF
	}
	return v.read(ctx)
}

func (v *vectorAggregationPipeline) read(ctx context.Context) (arrow.Record, error) {
	var (
		labelValues = make([]string, len(v.groupBy))
	)

	v.aggregator.Reset() // reset before reading new inputs
	inputsExhausted := false
	for !inputsExhausted {
		inputsExhausted = true

		for _, input := range v.inputs {
			record, err := input.Read(ctx)
			if err != nil {
				if errors.Is(err, EOF) {
					continue
				}
				return nil, err
			}

			inputsExhausted = false

			// extract timestamp column
			tsVec, err := v.tsEval(record)
			if err != nil {
				return nil, err
			}
			tsCol := tsVec.ToArray().(*array.Timestamp)

			// extract value column
			valueVec, err := v.valueEval(record)
			if err != nil {
				return nil, err
			}
			valueArr := valueVec.ToArray().(*array.Int64)

			// extract all the columns that are used for grouping
			arrays := make([]*array.String, 0, len(v.groupBy))
			for _, columnExpr := range v.groupBy {
				vec, err := v.evaluator.eval(columnExpr, record)
				if err != nil {
					return nil, err
				}

				if vec.Type() != datatype.Loki.String {
					return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.Type())
				}

				arrays = append(arrays, vec.ToArray().(*array.String))
			}

			for row := range int(record.NumRows()) {
				// reset for each row
				clear(labelValues)
				for col, arr := range arrays {
					labelValues[col] = arr.Value(row)
				}

				v.aggregator.Add(tsCol.Value(row).ToTime(arrow.Nanosecond), valueArr.Value(row), labelValues)
			}
		}
	}

	v.inputsExhausted = true

	return v.aggregator.BuildRecord()
}

// Close closes the resources of the pipeline.
func (v *vectorAggregationPipeline) Close() {
	for _, input := range v.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (v *vectorAggregationPipeline) Inputs() []Pipeline {
	return v.inputs
}

// Transport returns the transport type of the pipeline.
func (v *vectorAggregationPipeline) Transport() Transport {
	return Local
}
