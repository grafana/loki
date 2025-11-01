package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type marshaler interface {
	MarshalPhysical() (physical.Expression, error)
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression) MarshalPhysical() (physical.Expression, error) {
	m, ok := e.Kind.(marshaler)
	if !ok {
		return nil, fmt.Errorf("unsupported physical expression type: %T", e.Kind)
	}
	return m.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression_Unary) MarshalPhysical() (physical.Expression, error) {
	return e.Unary.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression_Binary) MarshalPhysical() (physical.Expression, error) {
	return e.Binary.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression_Variadic) MarshalPhysical() (physical.Expression, error) {
	return e.Variadic.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression_Literal) MarshalPhysical() (physical.Expression, error) {
	return e.Literal.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *Expression_Column) MarshalPhysical() (physical.Expression, error) {
	return e.Column.MarshalPhysical()
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *UnaryExpression) MarshalPhysical() (physical.Expression, error) {
	value, err := e.Value.MarshalPhysical()
	if err != nil {
		return nil, err
	}

	op, err := e.Op.MarshalType()
	if err != nil {
		return nil, err
	}

	return &physical.UnaryExpr{
		Op:   op,
		Left: value,
	}, nil
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *BinaryExpression) MarshalPhysical() (physical.Expression, error) {
	left, err := e.Left.MarshalPhysical()
	if err != nil {
		return nil, err
	}
	right, err := e.Right.MarshalPhysical()
	if err != nil {
		return nil, err
	}

	op, err := e.Op.MarshalType()
	if err != nil {
		return nil, err
	}

	return &physical.BinaryExpr{
		Op:    op,
		Left:  left,
		Right: right,
	}, nil
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *VariadicExpression) MarshalPhysical() (physical.Expression, error) {
	expressions := make([]physical.Expression, len(e.Args))
	for i, arg := range e.Args {
		expr, err := arg.MarshalPhysical()
		if err != nil {
			return nil, err
		}
		expressions[i] = expr
	}

	op, err := e.Op.MarshalType()
	if err != nil {
		return nil, err
	}

	return &physical.VariadicExpr{
		Op:          op,
		Expressions: expressions,
	}, nil
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression) MarshalPhysical() (physical.Expression, error) {
	m, ok := e.Kind.(literalMarshaler)
	if !ok {
		return nil, fmt.Errorf("unsupported literal expression type: %T", e.Kind)
	}
	literal, err := m.MarshalLiteral()
	if err != nil {
		return nil, err
	}

	return &physical.LiteralExpr{Literal: literal}, nil
}

// MarshalPhysical converts a protobuf expression into a physical plan
// expression. Returns an error if the conversion fails or is unsupported.
func (e *ColumnExpression) MarshalPhysical() (physical.Expression, error) {
	columnType, err := e.Type.MarshalType()
	if err != nil {
		return nil, err
	}

	return &physical.ColumnExpr{
		Ref: types.ColumnRef{
			Column: e.Name,
			Type:   columnType,
		},
	}, nil
}
