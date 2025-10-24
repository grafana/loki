package executor

import (
	"context"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
)

func NewProjectPipeline(input Pipeline, proj *physicalpb.Projection, evaluator *expressionEvaluator, allocator memory.Allocator) (Pipeline, error) {
	// Shortcut for ALL=true DROP=false EXPAND=false
	if proj.All && !proj.Drop && !proj.Expand {
		return input, nil
	}

	// Get the column names from the projection expressions
	colRefs := make([]physicalpb.ColumnExpression, 0, len(proj.Expressions))
	unaryExprs := make([]physicalpb.UnaryExpression, 0, len(proj.Expressions))

	for i, expr := range proj.Expressions {
		switch expr := expr.Kind.(type) {
		case *physicalpb.Expression_ColumnExpression:
			colRefs = append(colRefs, *expr.ColumnExpression)
		case *physicalpb.Expression_UnaryExpression:
			unaryExprs = append(unaryExprs, *expr.UnaryExpression)
		default:
			return nil, fmt.Errorf("projection expression %d is unsupported", i)
		}
	}

	// Create KEEP projection pipeline:
	// Drop all columns except the ones referenced in proj.Expressions.
	if !proj.All && !proj.Drop && !proj.Expand {
		return newKeepPipeline(colRefs, func(refs []physicalpb.ColumnExpression, ident *semconv.Identifier) bool {
			return slices.ContainsFunc(refs, func(ref physicalpb.ColumnExpression) bool {
				// Keep all of the ambiguous columns
				if ref.Type == physicalpb.COLUMN_TYPE_AMBIGUOUS {
					return ref.Name == ident.ShortName()
				}
				// Keep only if type matches
				return ref.Name == ident.ShortName() && ref.Type == ident.ColumnType()
			})
		}, input)
	}

	// Create DROP projection pipeline:
	// Keep all columns except the ones referenced in proj.Expressions.
	if proj.All && proj.Drop {
		return newKeepPipeline(colRefs, func(refs []physicalpb.ColumnExpression, ident *semconv.Identifier) bool {
			return !slices.ContainsFunc(refs, func(ref physicalpb.ColumnExpression) bool {
				// Drop all of the ambiguous columns
				if ref.Type == physicalpb.COLUMN_TYPE_AMBIGUOUS {
					return ref.Name == ident.ShortName()
				}
				// Drop only if type matches
				return ref.Name == ident.ShortName() && ref.Type == ident.ColumnType()
			})
		}, input)
	}

	// Create EXPAND projection pipeline:
	// Keep all columns and expand the ones referenced in proj.Expressions.
	// TODO: as implemented, epanding and keeping/dropping cannot happen in the same projection. Is this desired?
	if proj.All && proj.Expand {
		return newExpandPipeline(unaryExprs, evaluator, allocator, input)
	}

	return nil, errNotImplemented
}

func newKeepPipeline(colRefs []physicalpb.ColumnExpression, keepFunc func([]physicalpb.ColumnExpression, *semconv.Identifier) bool, input Pipeline) (*GenericPipeline, error) {
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

func newExpandPipeline(expressions []physicalpb.UnaryExpression, evaluator *expressionEvaluator, allocator memory.Allocator, input Pipeline) (*GenericPipeline, error) {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
		if len(inputs) != 1 {
			return nil, fmt.Errorf("expected 1 input, got %d", len(inputs))
		}
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}

		columns := []arrow.Array{}
		fields := []arrow.Field{}

		for i, field := range batch.Schema().Fields() {
			columns = append(columns, batch.Column(i))
			fields = append(fields, field)
		}

		for _, expr := range expressions {
			vec, err := evaluator.eval(*expr.ToExpression(), allocator, batch)
			if err != nil {
				return nil, err
			}
			if arrStruct, ok := vec.ToArray().(*array.Struct); ok {
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
