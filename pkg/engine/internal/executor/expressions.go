package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type expressionEvaluator struct{}

func newExpressionEvaluator() expressionEvaluator {
	return expressionEvaluator{}
}

func (e expressionEvaluator) eval(expr physical.Expression, input arrow.RecordBatch) (arrow.Array, error) {
	switch expr := expr.(type) {

	case *physical.LiteralExpr:
		return NewScalar(expr.Literal(), int(input.NumRows())), nil

	case *physical.ColumnExpr:
		colIdent := semconv.NewIdentifier(expr.Ref.Column, expr.Ref.Type, types.Loki.String)

		// For non-ambiguous columns, we can look up the column in the schema by its fully qualified name.
		if expr.Ref.Type != types.ColumnTypeAmbiguous {
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
		if expr.Ref.Type == types.ColumnTypeAmbiguous {
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

	case *physical.UnaryExpr:
		lhr, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}

		fn, err := unaryFunctions.GetForSignature(expr.Op, lhr.DataType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function: %w", err)
		}
		return fn.Evaluate(lhr)

	case *physical.BinaryExpr:
		lhs, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}

		rhs, err := e.eval(expr.Right, input)
		if err != nil {
			return nil, err
		}

		// At the moment we only support functions that accept the same input types.
		// TODO(chaudum): Compare Loki type, not Arrow type
		if lhs.DataType().ID() != rhs.DataType().ID() {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): types do not match", expr.Op, lhs.DataType(), rhs.DataType())
		}

		// TODO(chaudum): Resolve function by Loki type
		fn, err := binaryFunctions.GetForSignature(expr.Op, lhs.DataType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): %w", expr.Op, lhs.DataType(), rhs.DataType(), err)
		}

		// Check is lhs and rhs are Scalar vectors, because certain function types, such as regexp functions
		// can optimize the evaluation per batch.
		_, lhsIsScalar := expr.Left.(*physical.LiteralExpr)
		_, rhsIsScalar := expr.Right.(*physical.LiteralExpr)
		return fn.Evaluate(lhs, rhs, lhsIsScalar, rhsIsScalar)

	case *physical.VariadicExpr:
		args := make([]arrow.Array, len(expr.Expressions))
		for i, arg := range expr.Expressions {
			p, err := e.eval(arg, input)
			if err != nil {
				return nil, err
			}
			args[i] = p
		}

		fn, err := variadicFunctions.GetForSignature(expr.Op)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function: %w", err)
		}
		return fn.Evaluate(args...)
	}

	return nil, fmt.Errorf("unknown expression: %v", expr)
}

// newFunc returns a new function that can evaluate an input against a binded expression.
func (e expressionEvaluator) newFunc(expr physical.Expression) evalFunc {
	return func(input arrow.RecordBatch) (arrow.Array, error) {
		return e.eval(expr, input)
	}
}

type evalFunc func(input arrow.RecordBatch) (arrow.Array, error)

type columnWithType struct {
	col arrow.Array
	ct  types.ColumnType
}
