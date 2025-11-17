package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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
	grouping   physical.Grouping

	tsEval    evalFunc // used to evaluate the timestamp column
	valueEval evalFunc // used to evaluate the value column
}

var (
	vectorAggregationOperations = map[types.VectorAggregationType]aggregationOperation{
		types.VectorAggregationTypeSum:    aggregationOperationSum,
		types.VectorAggregationTypeCount:  aggregationOperationCount,
		types.VectorAggregationTypeMax:    aggregationOperationMax,
		types.VectorAggregationTypeMin:    aggregationOperationMin,
		types.VectorAggregationTypeAvg:    aggregationOperationAvg,
		types.VectorAggregationTypeStddev: aggregationOperationStddev,
		types.VectorAggregationTypeStdvar: aggregationOperationStdvar,
	}
)

func newVectorAggregationPipeline(inputs []Pipeline, grouping physical.Grouping, evaluator expressionEvaluator, operation types.VectorAggregationType) (*vectorAggregationPipeline, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("vector aggregation expects at least one input")
	}

	op, ok := vectorAggregationOperations[operation]
	if !ok {
		panic(fmt.Sprintf("unknown vector aggregation operation: %v", operation))
	}

	return &vectorAggregationPipeline{
		inputs:     inputs,
		evaluator:  evaluator,
		grouping:   grouping,
		aggregator: newAggregator(0, op),
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
func (v *vectorAggregationPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if v.inputsExhausted {
		return nil, EOF
	}
	return v.read(ctx)
}

func (v *vectorAggregationPipeline) read(ctx context.Context) (arrow.RecordBatch, error) {
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
			tsCol := tsVec.(*array.Timestamp)

			// extract value column
			valueVec, err := v.valueEval(record)
			if err != nil {
				return nil, err
			}
			valueArr := valueVec.(*array.Float64)

			// extract all the columns that are used for grouping
			var arrays []*array.String
			var fields []arrow.Field
			switch v.grouping.Mode {
			case types.GroupingModeByLabelSet:
				// Gouping by a label set. Take only labels from that set.
				arrays = make([]*array.String, 0, len(v.grouping.Columns))
				for _, columnExpr := range v.grouping.Columns {
					vec, err := v.evaluator.eval(columnExpr, record)
					if err != nil {
						return nil, err
					}

					if vec.DataType().ID() != types.Arrow.String.ID() {
						return nil, fmt.Errorf("unsupported datatype for grouping %s", vec.DataType())
					}

					arr := vec.(*array.String)
					arrays = append(arrays, arr)

					colExpr, ok := columnExpr.(*physical.ColumnExpr)
					if !ok {
						return nil, fmt.Errorf("invalid column expression type %T", columnExpr)
					}
					ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
					fields = append(fields, semconv.FieldFromIdent(ident, true))
				}
			case types.GroupingModeByEmptySet:
				// Gouping by an empty set. Group all into one.
				arrays = make([]*array.String, 0)
				fields = make([]arrow.Field, 0)
			case types.GroupingModeWithoutLabelSet:
				// Grouping without a lable set. Exclude lables from that set.
				schema := record.Schema()
				for i, field := range schema.Fields() {
					found := false
					for _, g := range v.grouping.Columns {
						colExpr, ok := g.(*physical.ColumnExpr)
						if !ok {
							return nil, fmt.Errorf("unknown column expression %v", g)
						}
						if colExpr.Ref.Column == field.Name {
							found = true
							break
						}
					}
					if !found {
						arrays = append(arrays, record.Column(i).(*array.String))
						fields = append(fields, field)
					}
				}
			case types.GroupingModeWithoutEmptySet:
				// No grouping. Take all string columns from the record as-is.
				schema := record.Schema()
				for i, field := range schema.Fields() {
					ident, err := semconv.ParseFQN(field.Name)
					if err != nil {
						return nil, err
					}
					if ident.ColumnType() == types.ColumnTypeLabel ||
						ident.ColumnType() == types.ColumnTypeMetadata ||
						ident.ColumnType() == types.ColumnTypeParsed {
						arrays = append(arrays, record.Column(i).(*array.String))
						fields = append(fields, field)
					}
				}
			}

			for row := range int(record.NumRows()) {
				labelValues := make([]string, 0, len(arrays))
				labels := make([]arrow.Field, 0, len(arrays))
				for i, arr := range arrays {
					val := arr.Value(row)
					if val != "" {
						labelValues = append(labelValues, val)
						labels = append(labels, fields[i])
					}
				}

				v.aggregator.Add(tsCol.Value(row).ToTime(arrow.Nanosecond), valueArr.Value(row), labels, labelValues)
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
