package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func NewMathExpressionPipeline(expr *physical.MathExpression, inputs []Pipeline, evaluator expressionEvaluator) *GenericPipeline {
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Works only with a single input for now.
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}
		defer batch.Release()

		// TODO make sure all inputs matches on timestamps

		fields := make([]arrow.Field, 0, len(inputs))
		for i := range inputs {
			fields = append(fields, arrow.Field{
				Name:     fmt.Sprintf("input_%d", i),
				Type:     datatype.Arrow.Float,
				Nullable: false,
				Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Float),
			})
		}

		valColumnExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}
		cols := make([]arrow.Array, 0, len(inputs))
		inputVec, err := evaluator.eval(valColumnExpr, batch) // TODO read input[i] instead of batch
		if err != nil {
			return failureState(err)
		}
		inputData := inputVec.ToArray()
		if inputData.DataType().ID() != arrow.FLOAT64 {
			return failureState(fmt.Errorf("expression returned non-float64 type %s", inputData.DataType()))
		}
		inputCol := inputData.(*array.Float64)
		cols = append(cols, inputCol)

		inputSchema := arrow.NewSchema(fields, nil)
		evalInput := array.NewRecord(inputSchema, cols, batch.NumRows())
		defer evalInput.Release()

		res, err := evaluator.eval(expr.Expression, evalInput)
		if err != nil {
			return failureState(err)
		}
		data := res.ToArray()
		if data.DataType().ID() != arrow.FLOAT64 {
			return failureState(fmt.Errorf("expression returned non-float64 type %s", data.DataType()))
		}
		valCol := data.(*array.Float64)

		tsColumnExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameBuiltinTimestamp,
				Type:   types.ColumnTypeBuiltin,
			},
		}
		tsVec, err := evaluator.eval(tsColumnExpr, batch)
		if err != nil {
			return failureState(err)
		}
		tsCol := tsVec.ToArray().(*array.Timestamp)

		outputSchema := arrow.NewSchema([]arrow.Field{
			{
				Name:     types.ColumnNameBuiltinTimestamp,
				Type:     datatype.Arrow.Timestamp,
				Nullable: false,
				Metadata: datatype.ColumnMetadataBuiltinTimestamp,
			},
			{
				Name:     types.ColumnNameGeneratedValue,
				Type:     datatype.Arrow.Float,
				Nullable: false,
				Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Float),
			},
		}, nil)
		evaluatedRecord := array.NewRecord(outputSchema, []arrow.Array{tsCol, valCol}, batch.NumRows())

		return successState(evaluatedRecord)
	}, inputs...)
}
