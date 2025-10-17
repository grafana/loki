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

func NewProjectPipeline(input Pipeline, proj *physical.Projection, _ *expressionEvaluator) (Pipeline, error) {
	// Shortcut for ALL=true DROP=false EXPAND=false
	if proj.All && !proj.Drop && !proj.Expand {
		return input, nil
	}

	// Get the column names from the projection expressions
	colRefs := make([]types.ColumnRef, len(proj.Expressions))

	for i, col := range proj.Expressions {
		if colExpr, ok := col.(*physical.ColumnExpr); ok {
			colRefs[i] = colExpr.Ref
		} else {
			return nil, fmt.Errorf("projection column %d is not a column expression", i)
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
	if proj.All && proj.Expand {
		return nil, errNotImplemented
	}

	return nil, errNotImplemented
}

func newKeepPipeline(colRefs []types.ColumnRef, keepFunc func([]types.ColumnRef, *semconv.Identifier) bool, input Pipeline) (*GenericPipeline, error) {
	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
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

		schema := arrow.NewSchema(fields, nil)
		return array.NewRecord(schema, columns, batch.NumRows()), nil
	}, input), nil
}
