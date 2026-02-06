package executor

import (
	"context"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func NewProjectPipeline(input Pipeline, proj *physical.Projection, evaluator *expressionEvaluator, region *xcap.Region) (Pipeline, error) {
	// Shortcut for ALL=true DROP=false EXPAND=false
	if proj.All && !proj.Drop && !proj.Expand {
		return input, nil
	}

	// Get the column names from the projection expressions
	colRefs := make([]types.ColumnRef, 0, len(proj.Expressions))
	expandExprs := make([]physical.Expression, 0, len(proj.Expressions))

	for i, expr := range proj.Expressions {
		switch expr := expr.(type) {
		case *physical.ColumnExpr:
			colRefs = append(colRefs, expr.Ref)
		case *physical.UnaryExpr:
			expandExprs = append(expandExprs, expr)
		case *physical.BinaryExpr:
			expandExprs = append(expandExprs, expr)
		case *physical.VariadicExpr:
			expandExprs = append(expandExprs, expr)
		default:
			return nil, fmt.Errorf("projection expression %d is unsupported", i)
		}
	}

	if len(expandExprs) > 1 {
		return nil, fmt.Errorf("there might be only one math expression for `value` column at a time")
	}

	// Create KEEP projection pipeline:
	// Drop all columns except the ones referenced in proj.Expressions.
	if !proj.All && !proj.Drop && !proj.Expand {
		return newKeepPipeline(colRefs, func(refs []types.ColumnRef, ident *semconv.Identifier) bool {
			return slices.ContainsFunc(refs, func(ref types.ColumnRef) bool {
				// Keep all of the ambiguous columns
				if ref.Type == types.ColumnTypeAmbiguous {
					return ref.Column == ident.ShortName()
				}
				// Keep only if type matches
				return ref.Column == ident.ShortName() && ref.Type == ident.ColumnType()
			})
		}, input, region)
	}

	// Create DROP projection pipeline:
	// Keep all columns except the ones referenced in proj.Expressions.
	if proj.All && proj.Drop {
		return newKeepPipeline(colRefs, func(refs []types.ColumnRef, ident *semconv.Identifier) bool {
			return !slices.ContainsFunc(refs, func(ref types.ColumnRef) bool {
				// Drop all of the ambiguous columns
				if ref.Type == types.ColumnTypeAmbiguous {
					return ref.Column == ident.ShortName()
				}
				// Drop only if type matches
				return ref.Column == ident.ShortName() && ref.Type == ident.ColumnType()
			})
		}, input, region)
	}

	// Create EXPAND projection pipeline:
	// Keep all columns and expand the ones referenced in proj.Expressions.
	// TODO: as implemented, epanding and keeping/dropping cannot happen in the same projection. Is this desired?
	if proj.All && proj.Expand && len(expandExprs) > 0 {
		return newExpandPipeline(expandExprs[0], evaluator, input, region)
	}

	return nil, errNotImplemented
}

func newKeepPipeline(colRefs []types.ColumnRef, keepFunc func([]types.ColumnRef, *semconv.Identifier) bool, input Pipeline, region *xcap.Region) (*GenericPipeline, error) {
	identCache := semconv.NewIdentifierCache()

	return newGenericPipelineWithRegion(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		if len(inputs) != 1 {
			return nil, fmt.Errorf("expected 1 input, got %d", len(inputs))
		}
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}

		if batch.NumRows() == 0 {
			// Nothing to process, return an empty record with the same schema
			return batch, nil
		}

		columns := make([]arrow.Array, 0, batch.NumCols())
		fields := make([]arrow.Field, 0, batch.NumCols())

		for i, field := range batch.Schema().Fields() {
			ident, err := identCache.ParseFQN(field.Name)
			if err != nil {
				return nil, err
			}
			if keepFunc(colRefs, ident) {
				columns = append(columns, batch.Column(i))
				fields = append(fields, field)
			}
		}

		metadata := batch.Schema().Metadata()
		schema := arrow.NewSchema(fields, &metadata)
		return array.NewRecordBatch(schema, columns, batch.NumRows()), nil
	}, region, input), nil
}

func newExpandPipeline(expr physical.Expression, evaluator *expressionEvaluator, input Pipeline, region *xcap.Region) (*GenericPipeline, error) {
	identCache := semconv.NewIdentifierCache()

	return newGenericPipelineWithRegion(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		if len(inputs) != 1 {
			return nil, fmt.Errorf("expected 1 input, got %d", len(inputs))
		}
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}

		if batch.NumRows() == 0 {
			// Nothing to process, return an empty record with the same schema
			return batch, nil
		}

		outputFields := make([]arrow.Field, 0)
		outputCols := make([]arrow.Array, 0)
		schema := batch.Schema()

		// move all columns into the output except `value`
		for i, field := range batch.Schema().Fields() {
			ident, err := identCache.ParseFQN(schema.Field(i).Name)
			if err != nil {
				return nil, err
			}
			if !ident.Equal(semconv.ColumnIdentValue) {
				outputCols = append(outputCols, batch.Column(i))
				outputFields = append(outputFields, field)
			}
		}

		vec, err := evaluator.eval(expr, batch)
		if err != nil {
			return nil, err
		}

		if vec == nil {
			return batch, nil
		}

		switch arrCasted := vec.(type) {
		case *array.Struct:
			structSchema, ok := arrCasted.DataType().(*arrow.StructType)
			if !ok {
				return nil, fmt.Errorf("unexpected type returned from evaluation, expected *arrow.StructType, got %T", arrCasted.DataType())
			}
			for i := range arrCasted.NumField() {
				newField := structSchema.Field(i)
				if idx := slices.IndexFunc(outputFields, func(f arrow.Field) bool {
					return f.Name == newField.Name
				}); idx != -1 {
					outputCols[idx] = mergeColumns(outputCols[idx], arrCasted.Field(i))
					outputFields[idx] = newField
				} else {
					outputCols = append(outputCols, arrCasted.Field(i))
					outputFields = append(outputFields, newField)
				}
			}
		case *array.Float64:
			outputFields = append(outputFields, semconv.FieldFromIdent(semconv.ColumnIdentValue, false))
			outputCols = append(outputCols, arrCasted)
		default:
			return nil, fmt.Errorf("unexpected type returned from evaluation %T", arrCasted.DataType())
		}

		metadata := schema.Metadata()
		outputSchema := arrow.NewSchema(outputFields, &metadata)
		return array.NewRecordBatch(outputSchema, outputCols, batch.NumRows()), nil
	}, region, input), nil
}

// mergeColumns merges two columns by preferring non-null and non-empty values from the new column (b).
// If b has a null or empty value at index i, keep the value from a at that index.
// If b has a non-null and non-empty value at index i, use the value from b (overwriting a).
func mergeColumns(a, b arrow.Array) arrow.Array {
	// Only handle string arrays for now (which is what parsers produce)
	aStr, aOk := a.(*array.String)
	bStr, bOk := b.(*array.String)

	if !aOk || !bOk {
		// If not both strings, just return b (overwrite behavior)
		return b
	}

	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Reserve(aStr.Len())

	for i := range aStr.Len() {
		if bStr.IsNull(i) || bStr.Value(i) == "" {
			// New value is null or empty, keep old value
			if aStr.IsNull(i) {
				builder.AppendNull()
			} else {
				builder.Append(aStr.Value(i))
			}
		} else {
			builder.Append(bStr.Value(i))
		}
	}

	return builder.NewStringArray()
}
