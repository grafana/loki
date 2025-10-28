package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type expressionEvaluator struct{}

func newExpressionEvaluator() expressionEvaluator {
	return expressionEvaluator{}
}

func (e expressionEvaluator) eval(expr physicalpb.Expression, input arrow.Record) (arrow.Array, error) {

	switch expr := expr.Kind.(type) {

	case *physicalpb.Expression_LiteralExpression:
		return NewScalar(expr.LiteralExpression.Kind, int(input.NumRows())), nil

	case *physicalpb.Expression_ColumnExpression:
		colIdent := semconv.NewIdentifier(expr.ColumnExpression.Name, expr.ColumnExpression.Type, types.Loki.String)

		// For non-ambiguous columns, we can look up the column in the schema by its fully qualified name.
		if expr.ColumnExpression.Type != physicalpb.COLUMN_TYPE_AMBIGUOUS {
			for idx, field := range input.Schema().Fields() {
				ident, err := semconv.ParseFQN(field.Name)
				if err != nil {
					return nil, fmt.Errorf("failed to parse column %s: %w", field.Name, err)
				}
				if ident.ShortName() == colIdent.ShortName() && ident.ColumnType() == colIdent.ColumnType() {
					return input.Column(idx), nil
				}
			}
		}

		// For ambiguous columns, we need to filter on the name and type and combine matching columns into a CoalesceVector.
		if expr.ColumnExpression.Type == physicalpb.COLUMN_TYPE_AMBIGUOUS {
			var fieldIndices []int
			var fieldIdents []*semconv.Identifier

			for idx, field := range input.Schema().Fields() {
				ident, err := semconv.ParseFQN(field.Name)
				if err != nil {
					return nil, fmt.Errorf("failed to parse column %s: %w", field.Name, err)
				}
				if ident.ShortName() == colIdent.ShortName() {
					fieldIndices = append(fieldIndices, idx)
					fieldIdents = append(fieldIdents, ident)
				}
			}

			// Collect all matching columns and order by precedence
			var vecs []*columnWithType
			for i := range fieldIndices {
				idx := fieldIndices[i]
				ident := fieldIdents[i]

				// TODO(ashwanth): Support other data types in CoalesceVector.
				// For now, ensure all vectors are strings to avoid type conflicts.
				if ident.DataType() != types.Loki.String {
					return nil, fmt.Errorf("column %s has datatype %s, but expression expects %s", ident.ShortName(), ident.DataType(), types.Loki.String)
				}
				vecs = append(vecs, &columnWithType{col: input.Column(idx), ct: ident.ColumnType()})
			}

			// Single column matches
			if len(vecs) == 1 {
				return vecs[0].col, nil
			}

			// Multiple columns match
			if len(vecs) > 1 {
				return NewCoalesce(vecs), nil
			}
		}

		// A non-existent column is represented as a string scalar with zero-value.
		// This reflects current behaviour, where a label filter `| foo=""` would match all if `foo` is not defined.
		return NewScalar(types.NewLiteral(""), int(input.NumRows())), nil

	case *physicalpb.Expression_UnaryExpression:
		lhr, err := e.eval(*expr.UnaryExpression.Value, input)
		if err != nil {
			return nil, err
		}

		fn, err := unaryFunctions.GetForSignature(expr.UnaryExpression.Op, lhr.DataType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function: %w", err)
		}
		return fn.Evaluate(lhr)

	case *physicalpb.Expression_BinaryExpression:
		lhs, err := e.eval(*expr.BinaryExpression.Left, input)
		if err != nil {
			return nil, err
		}

		rhs, err := e.eval(*expr.BinaryExpression.Right, input)
		if err != nil {
			return nil, err
		}

		// At the moment we only support functions that accept the same input types.
		// TODO(chaudum): Compare Loki type, not Arrow type
		if lhs.DataType().ID() != rhs.DataType().ID() {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): types do not match", expr.BinaryExpression.Op, lhs.DataType(), rhs.DataType())
		}

		// TODO(chaudum): Resolve function by Loki type
		fn, err := binaryFunctions.GetForSignature(expr.BinaryExpression.Op, lhs.DataType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): %w", expr.BinaryExpression.Op, lhs.DataType(), rhs.DataType(), err)
		}
		return fn.Evaluate(lhs, rhs)
	}

	return nil, fmt.Errorf("unknown expression: %v", expr)
}

// newFunc returns a new function that can evaluate an input against a binded expression.
func (e expressionEvaluator) newFunc(expr physical.Expression) evalFunc {
	return func(input arrow.Record) (arrow.Array, error) {
		return e.eval(expr, input)
	}
}

type evalFunc func(input arrow.Record) (arrow.Array, error)

type columnWithType struct {
	col arrow.Array
	ct  types.ColumnType
}
