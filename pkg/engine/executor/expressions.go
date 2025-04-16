package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type expressionEvaluator struct{}

func (e *expressionEvaluator) eval(expr physical.Expression, input arrow.Record) (ColumnVector, error) {
	switch expr := expr.(type) {

	case *physical.LiteralExpr:
		return &Scalar{
			value: expr.Value,
			rows:  input.NumRows(),
		}, nil

	case *physical.ColumnExpr:
		for i := range input.NumCols() {
			if input.ColumnName(int(i)) == expr.Ref.Column {
				return &Array{
					array: input.Column(int(i)),
					rows:  input.NumRows(),
				}, nil
			}
		}
		return nil, fmt.Errorf("unknown column %s: %w", expr.Ref.String(), errors.ErrKey)

	case *physical.UnaryExpr:
		_, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to evaluate unary expression: %w", errors.ErrNotImplemented)

	case *physical.BinaryExpr:
		_, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		_, err = e.eval(expr.Right, input)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to evaluate binary expression: %w", errors.ErrNotImplemented)

	}

	return nil, fmt.Errorf("unknown expression: %v", expr)
}

// ColumnVector represents columnar values from evaluated expressions.
type ColumnVector interface {
	// ToArray returns the underlying Arrow array representation of the column vector.
	ToArray() arrow.Array
	// Value returns the value at the specified index position in the column vector.
	Value(i int64) any
	// Type returns the Arrow data type of the column vector.
	Type() arrow.Type
}

// Scalar represents a single value repeated any number of times.
type Scalar struct {
	value types.Literal
	rows  int64
}

var _ ColumnVector = (*Scalar)(nil)

// ToArray implements ColumnVector.
func (v *Scalar) ToArray() arrow.Array {
	return nil
}

// Value implements ColumnVector.
func (v *Scalar) Value(i int64) any {
	return v.value.Value
}

// Type implements ColumnVector.
func (v Scalar) Type() arrow.Type {
	switch v.value.ValueType() {
	case types.ValueTypeBool:
		return arrow.BOOL
	case types.ValueTypeStr:
		return arrow.STRING
	case types.ValueTypeInt:
		return arrow.INT64
	case types.ValueTypeFloat:
		return arrow.FLOAT64
	case types.ValueTypeTimestamp:
		return arrow.UINT64
	default:
		return arrow.NULL
	}
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	rows  int64
}

// ToArray implements ColumnVector.
func (a *Array) ToArray() arrow.Array {
	return a.array
}

// Value implements ColumnVector.
func (a *Array) Value(i int64) any {
	return a.array.ValueStr(int(i))
}

// Type implements ColumnVector.
func (a *Array) Type() arrow.Type {
	return a.array.DataType().ID()
}

var _ ColumnVector = (*Array)(nil)
