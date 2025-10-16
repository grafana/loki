package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func NewMathExpressionPipeline(expr *physical.MathExpression, inputs []Pipeline, evaluator expressionEvaluator) *GenericPipeline {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
		// Works only with a single input for now.
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer batch.Release()

		fields := make([]arrow.Field, 0, len(inputs))
		fields = append(fields, semconv.FieldFromIdent(
			semconv.NewIdentifier(fmt.Sprintf("input_%d", 0), types.ColumnTypeGenerated, types.Loki.Float),
			false))

		valColumnExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: types.ColumnNameGeneratedValue,
				Type:   types.ColumnTypeGenerated,
			},
		}
		cols := make([]arrow.Array, 0, len(inputs))
		inputVec, err := evaluator.eval(valColumnExpr, batch) // TODO read input[i] instead of batch
		if err != nil {
			return nil, err
		}
		inputData := inputVec.ToArray()
		if inputData.DataType().ID() != arrow.FLOAT64 {
			return nil, fmt.Errorf("expression returned non-float64 type %s", inputData.DataType())
		}
		inputCol := inputData.(*array.Float64)
		defer inputCol.Release()

		cols = append(cols, inputCol)

		inputSchema := arrow.NewSchema(fields, nil)
		evalInput := array.NewRecord(inputSchema, cols, batch.NumRows())
		defer evalInput.Release()

		res, err := evaluator.eval(expr.Expression, evalInput)
		if err != nil {
			return nil, err
		}
		data := res.ToArray()
		if data.DataType().ID() != arrow.FLOAT64 {
			return nil, fmt.Errorf("expression returned non-float64 type %s", data.DataType())
		}
		valCol := data.(*array.Float64)
		defer valCol.Release()

		schema := batch.Schema()
		outputFields := make([]arrow.Field, 0, schema.NumFields())
		outputCols := make([]arrow.Array, 0, schema.NumFields())
		for i := 0; i < schema.NumFields(); i++ {
			field := schema.Field(i)
			if field.Name != semconv.ColumnIdentValue.FQN() {
				outputFields = append(outputFields, field)
				outputCols = append(outputCols, batch.Column(i))
			}
		}
		outputFields = append(outputFields, semconv.FieldFromIdent(semconv.ColumnIdentValue, false))
		outputCols = append(outputCols, valCol)
		outputSchema := arrow.NewSchema(outputFields, nil)

		evaluatedRecord := array.NewRecord(outputSchema, outputCols, batch.NumRows())

		return evaluatedRecord, nil
	}, inputs...)
}
