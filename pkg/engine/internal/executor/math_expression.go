package executor

import (
	"context"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func NewMathExpressionPipeline(expr *physical.MathExpression, input Pipeline, evaluator expressionEvaluator) *GenericPipeline {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer batch.Release()

		res, err := evaluator.eval(expr.Expression, batch)
		if err != nil {
			return nil, err
		}
		data := res.ToArray()
		if data.DataType().ID() != arrow.FLOAT64 {
			return nil, fmt.Errorf("expression returned non-float64 type %s", data.DataType())
		}
		valCol := data.(*array.Float64)
		defer valCol.Release()

		// Build output by moving all columns from the input except columns that were used for expression evaluation
		// and add the result `value` column. In case of simple math expressions this will replace `value` with `value`.
		// In case of math expressions that go after Joins this will replace `value_left` and `value_right` with `value`.
		allColumnRefs := getAllColumnRefs(expr.Expression)
		schema := batch.Schema()
		outputFields := make([]arrow.Field, 0, schema.NumFields()+1)
		outputCols := make([]arrow.Array, 0, schema.NumFields()+1)
		for i := 0; i < schema.NumFields(); i++ {
			if !slices.ContainsFunc(allColumnRefs, func(ref *types.ColumnRef) bool {
				ident, err := semconv.ParseFQN(schema.Field(i).Name)
				if err != nil {
					return false
				}
				return ref.Column == ident.ShortName()
			}) {
				outputFields = append(outputFields, schema.Field(i))
				outputCols = append(outputCols, batch.Column(i))
			}
		}

		// Add `values` column
		outputFields = append(outputFields, semconv.FieldFromIdent(semconv.ColumnIdentValue, false))
		outputCols = append(outputCols, valCol)

		outputSchema := arrow.NewSchema(outputFields, nil)
		evaluatedRecord := array.NewRecord(outputSchema, outputCols, batch.NumRows())

		return evaluatedRecord, nil
	}, input)
}

// getAllColumnRefs finds all column ref from the expression. Should 1 or 2, but might be deep in the expression tree.
func getAllColumnRefs(expr physical.Expression) []*types.ColumnRef {
	switch expr := expr.(type) {
	case *physical.BinaryExpr:
		return append(getAllColumnRefs(expr.Left), getAllColumnRefs(expr.Right)...)
	case *physical.UnaryExpr:
		return getAllColumnRefs(expr.Left)
	case *physical.ColumnExpr:
		return []*types.ColumnRef{&expr.Ref}
	}

	return nil
}
