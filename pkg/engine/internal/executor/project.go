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
)

func NewProjectPipeline(input Pipeline, proj *physical.Projection, evaluator *expressionEvaluator, allocator memory.Allocator) (Pipeline, error) {
	// Shortcut for ALL=true DROP=false EXPAND=false
	if proj.All && !proj.Drop && !proj.Expand {
		return input, nil
	}

	// Get the column names from the projection expressions
	colRefs := make([]types.ColumnRef, 0, len(proj.Expressions))
	mathExprs := make([]physical.Expression, 0, len(proj.Expressions))

	for i, expr := range proj.Expressions {
		switch expr := expr.(type) {
		case *physical.ColumnExpr:
			colRefs = append(colRefs, expr.Ref)
		case *physical.UnaryExpr:
			mathExprs = append(mathExprs, expr)
		case *physical.BinaryExpr:
			mathExprs = append(mathExprs, expr)
		default:
			return nil, fmt.Errorf("projection expression %d is unsupported", i)
		}
	}

	if len(mathExprs) > 1 {
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
		}, input)
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
		}, input)
	}

	// Create EXPAND projection pipeline:
	// Keep all columns and expand the ones referenced in proj.Expressions.
	// TODO: as implmented, epanding and keeping/dropping cannot happen in the same projection. Is this desired?
	if proj.All && proj.Expand && len(mathExprs) > 0 {
		return newExpandPipeline(mathExprs[0], evaluator, allocator, input)
	}

	return nil, errNotImplemented
}

func newKeepPipeline(colRefs []types.ColumnRef, keepFunc func([]types.ColumnRef, *semconv.Identifier) bool, input Pipeline) (*GenericPipeline, error) {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
		if len(inputs) != 1 {
			return nil, fmt.Errorf("expected 1 input, got %d", len(inputs))
		}
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer batch.Release()

		columns := make([]arrow.Array, 0, batch.NumCols())
		fields := make([]arrow.Field, 0, batch.NumCols())

		for i, field := range batch.Schema().Fields() {
			ident, err := semconv.ParseFQN(field.Name)
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
		return array.NewRecord(schema, columns, batch.NumRows()), nil
	}, input), nil
}

func newExpandPipeline(expr physical.Expression, evaluator *expressionEvaluator, allocator memory.Allocator, input Pipeline) (*GenericPipeline, error) {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
		if len(inputs) != 1 {
			return nil, fmt.Errorf("expected 1 input, got %d", len(inputs))
		}
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer batch.Release()

		outputFields := make([]arrow.Field, 0)
		outputCols := make([]arrow.Array, 0)
		schema := batch.Schema()

		// move all columns into the output except `value`
		for i, field := range batch.Schema().Fields() {
			ident, err := semconv.ParseFQN(schema.Field(i).Name)
			if err != nil {
				return nil, err
			}
			if !ident.Equal(semconv.ColumnIdentValue) {
				outputCols = append(outputCols, batch.Column(i))
				outputFields = append(outputFields, field)
			}
		}

		vec, err := evaluator.eval(expr, allocator, batch)
		if err != nil {
			return nil, err
		}
		arr := vec.ToArray()
		defer arr.Release()

		switch arrCasted := arr.(type) {
		case *array.Struct:
			structSchema, ok := arrCasted.DataType().(*arrow.StructType)
			if !ok {
				return nil, fmt.Errorf("unexpected type returned from evaluation %T", arr.DataType())
			}
			for i := range arrCasted.NumField() {
				outputCols = append(outputCols, arrCasted.Field(i))
				outputFields = append(outputFields, structSchema.Field(i))
			}
		case *array.Float64:
			outputFields = append(outputFields, semconv.FieldFromIdent(semconv.ColumnIdentValue, false))
			outputCols = append(outputCols, arrCasted)
		default:
			return nil, fmt.Errorf("unexpected type returned from evaluation %T", arr.DataType())
		}

		metadata := schema.Metadata()
		outputSchema := arrow.NewSchema(outputFields, &metadata)
		return array.NewRecord(outputSchema, outputCols, batch.NumRows()), nil
	}, input), nil
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
