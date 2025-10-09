package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewProjectPipeline(input Pipeline, columns []physical.ColumnExpression, evaluator *expressionEvaluator) (*GenericPipeline, error) {
	// Get the column names from the projection expressions
	columnNames := make([]string, len(columns))

	for i, col := range columns {
		if colExpr, ok := col.(*physical.ColumnExpr); ok {
			columnNames[i] = colExpr.Ref.Column
		} else {
			return nil, fmt.Errorf("projection column %d is not a column expression", i)
		}
	}

	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}

		projected := make([]arrow.Array, 0, len(columns))
		fields := make([]arrow.Field, 0, len(columns))

		for i := range columns {
			vec, err := evaluator.eval(columns[i], batch)
			if err != nil {
				return failureState(err)
			}
			fields = append(fields, arrow.Field{Name: columnNames[i], Type: vec.Type().ArrowType(), Metadata: types.ColumnMetadata(vec.ColumnType(), vec.Type())})
			projected = append(projected, vec.ToArray())
		}

		schema := arrow.NewSchema(fields, nil)
		// Create a new record with only the projected columns
		// retain the projected columns in a new batch then release the original record.
		projectedRecord := array.NewRecord(schema, projected, batch.NumRows())
		batch.Release()
		return successState(projectedRecord)
	}, input), nil
}
