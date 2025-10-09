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
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}
		defer batch.Release()

		// short circuit if there are no columns to project, treat as a select *
		if len(columns) == 0 {
			projectedRecord := array.NewRecord(batch.Schema(), batch.Columns(), batch.NumRows())
			return successState(projectedRecord)
		}

		columnOrder := []string{}
		projected := map[string]arrow.Array{}
		fields := map[string]arrow.Field{}

		for _, col := range columns {
			vec, err := evaluator.eval(col, batch)
			if err != nil {
				return failureState(err)
			}

			switch col := col.(type) {
			case *physical.ColumnExpr:
				columnName := col.Ref.Column
				columnOrder = append(columnOrder, columnName)
				fields[columnName] = arrow.Field{
					Name:     columnName,
					Type:     vec.Type().ArrowType(),
					Metadata: types.ColumnMetadata(vec.ColumnType(), vec.Type()),
				}
				projected[columnName] = vec.ToArray()
			case *physical.UnwrapExpr:
				if arrStruct, ok := vec.ToArray().(*array.Struct); ok {
					defer arrStruct.Release()
					for i := range arrStruct.NumField() {
						currentField := arrStruct.Field(i)

						structSchema, ok := arrStruct.DataType().(*arrow.StructType)
						if !ok {
							return failureState(fmt.Errorf("unexpected type for struct field %d, got %T", i, arrStruct.DataType()))
						}
						field := structSchema.Field(i)

						if _, ok := fields[field.Fingerprint()]; ok {
							continue
						}

						columnOrder = append(columnOrder, field.Fingerprint())
						fields[field.Fingerprint()] = field
						projected[field.Fingerprint()] = currentField
					}
				}
			default:
				return failureState(fmt.Errorf("unknown expression: %v", col))
			}
		}

		fieldsArr := make([]arrow.Field, 0, len(fields))
		projectedArr := make([]arrow.Array, 0, len(projected))
		for _, column := range columnOrder {
			fieldsArr = append(fieldsArr, fields[column])
			projectedArr = append(projectedArr, projected[column])
		}

		schema := arrow.NewSchema(fieldsArr, nil)
		projectedRecord := array.NewRecord(schema, projectedArr, batch.NumRows())
		return successState(projectedRecord)
	}, input), nil
}
