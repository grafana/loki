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
					if newField.Name == semconv.ColumnIdentError.FQN() || newField.Name == semconv.ColumnIdentErrorDetails.FQN() {
						outputCols[idx] = mergeErrors(outputCols[idx].(*array.String), arrCasted.Field(i).(*array.String))
					} else {
						panic(fmt.Sprintf("column duplicates %s", newField.Name))
					}
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

// mergeErrors merges string columns into a semicolon separated list of values.
func mergeErrors(a, b *array.String) *array.String {
	builder := array.NewStringBuilder(memory.DefaultAllocator)
	builder.Reserve(a.Len())

	for i := range a.Len() {
		aVal := a.Value(i)
		bVal := b.Value(i)
		if bVal != "" {
			if aVal != "" {
				builder.Append(fmt.Sprintf("%s; %s", aVal, bVal))
			} else {
				builder.Append(bVal)
			}
		} else {
			builder.Append(aVal)
		}
	}

	return builder.NewStringArray()
}
