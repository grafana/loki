package executor

import (
	"context"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewProjectPipeline(input Pipeline, proj *physical.Projection, evaluator *expressionEvaluator) (Pipeline, error) {
	// Shortcut for ALL=true DROP=false EXPAND=false
	if proj.All && !proj.Drop && !proj.Expand {
		return input, nil
	}

	// Get the column names from the projection expressions
	colRefs := make([]types.ColumnRef, 0, len(proj.Expressions))
	unaryExprs := make([]physical.UnaryExpression, 0, len(proj.Expressions))

	for i, expr := range proj.Expressions {
		switch expr := expr.(type) {
		case *physical.ColumnExpr:
			colRefs = append(colRefs, expr.Ref)
		case *physical.UnaryExpr:
			unaryExprs = append(unaryExprs, expr)
		default:
			return nil, fmt.Errorf("projection expression %d is unsupported", i)
		}
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
	if proj.All && proj.Expand {
		return newExpandPipeline(unaryExprs, evaluator, input)
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

func newExpandPipeline(expressions []physical.UnaryExpression, evaluator *expressionEvaluator, input Pipeline) (*GenericPipeline, error) {
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

		columns := []arrow.Array{}
		fields := []arrow.Field{}

		for i, field := range batch.Schema().Fields() {
			columns = append(columns, batch.Column(i))
			fields = append(fields, field)
		}

		for _, expr := range expressions {
			vec, err := evaluator.eval(expr, batch)
			if err != nil {
				return nil, err
			}
			defer vec.Release()
			if arrStruct, ok := vec.ToArray().(*array.Struct); ok {
				defer arrStruct.Release()
				structSchema, ok := arrStruct.DataType().(*arrow.StructType)
				if !ok {
					return nil, fmt.Errorf("unexpected type returned from evaluation, expected *arrow.StructType, got %T", arrStruct.DataType())
				}

				for i := range arrStruct.NumField() {
					columns = append(columns, arrStruct.Field(i))
					fields = append(fields, structSchema.Field(i))
				}
			}
		}

		metadata := batch.Schema().Metadata()
		schema := arrow.NewSchema(fields, &metadata)
		return array.NewRecord(schema, columns, batch.NumRows()), nil
	}, input), nil
}
