package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type unmarshaler interface {
	UnmarshalPhysical(expr physical.Expression) error
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression) UnmarshalPhysical(from physical.Expression) error {
	switch from.(type) {
	case *physical.UnaryExpr:
		e.Kind = &Expression_Unary{}
	case *physical.BinaryExpr:
		e.Kind = &Expression_Binary{}
	case *physical.VariadicExpr:
		e.Kind = &Expression_Variadic{}
	case *physical.LiteralExpr:
		e.Kind = &Expression_Literal{}
	case *physical.ColumnExpr:
		e.Kind = &Expression_Column{}
	default:
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	u, ok := e.Kind.(unmarshaler)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}
	return u.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression_Unary) UnmarshalPhysical(from physical.Expression) error {
	e.Unary = new(UnaryExpression)
	return e.Unary.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression_Binary) UnmarshalPhysical(from physical.Expression) error {
	e.Binary = new(BinaryExpression)
	return e.Binary.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression_Variadic) UnmarshalPhysical(from physical.Expression) error {
	e.Variadic = new(VariadicExpression)
	return e.Variadic.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression_Literal) UnmarshalPhysical(from physical.Expression) error {
	e.Literal = new(LiteralExpression)
	return e.Literal.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *Expression_Column) UnmarshalPhysical(from physical.Expression) error {
	e.Column = new(ColumnExpression)
	return e.Column.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *UnaryExpression) UnmarshalPhysical(from physical.Expression) error {
	unary, ok := from.(*physical.UnaryExpr)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	*e = UnaryExpression{
		Value: new(Expression),
	}
	if err := e.Op.UnmarshalType(unary.Op); err != nil {
		return err
	}
	if err := e.Value.UnmarshalPhysical(unary.Left); err != nil {
		return err
	}
	return nil
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *BinaryExpression) UnmarshalPhysical(from physical.Expression) error {
	binary, ok := from.(*physical.BinaryExpr)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	*e = BinaryExpression{
		Left:  new(Expression),
		Right: new(Expression),
	}
	if err := e.Op.UnmarshalType(binary.Op); err != nil {
		return err
	}
	if err := e.Left.UnmarshalPhysical(binary.Left); err != nil {
		return err
	}
	if err := e.Right.UnmarshalPhysical(binary.Right); err != nil {
		return err
	}
	return nil
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *VariadicExpression) UnmarshalPhysical(from physical.Expression) error {
	variadic, ok := from.(*physical.VariadicExpr)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	*e = VariadicExpression{
		Args: make([]*Expression, len(variadic.Expressions)),
	}
	if err := e.Op.UnmarshalType(variadic.Op); err != nil {
		return err
	}
	for i, expr := range variadic.Expressions {
		e.Args[i] = new(Expression)
		if err := e.Args[i].UnmarshalPhysical(expr); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression) UnmarshalPhysical(from physical.Expression) error {
	literal, ok := from.(*physical.LiteralExpr)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	switch literal.Literal.(type) {
	case types.NullLiteral:
		e.Kind = &LiteralExpression_NullLiteral{}
	case types.BoolLiteral:
		e.Kind = &LiteralExpression_BoolLiteral{}
	case types.StringLiteral:
		e.Kind = &LiteralExpression_StringLiteral{}
	case types.IntegerLiteral:
		e.Kind = &LiteralExpression_IntegerLiteral{}
	case types.FloatLiteral:
		e.Kind = &LiteralExpression_FloatLiteral{}
	case types.TimestampLiteral:
		e.Kind = &LiteralExpression_TimestampLiteral{}
	case types.DurationLiteral:
		e.Kind = &LiteralExpression_DurationLiteral{}
	case types.BytesLiteral:
		e.Kind = &LiteralExpression_BytesLiteral{}
	case types.StringListLiteral:
		e.Kind = &LiteralExpression_StringListLiteral{}
	}

	u, ok := e.Kind.(literalUnmarshaler)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}
	return u.UnmarshalLiteral(literal.Literal)
}

// UnmarshalPhysical reads from into e. Returns an error if the conversion fails
// or is unsupported.
func (e *ColumnExpression) UnmarshalPhysical(from physical.Expression) error {
	column, ok := from.(*physical.ColumnExpr)
	if !ok {
		return fmt.Errorf("unsupported physical expression type: %T", from)
	}

	*e = ColumnExpression{
		Name: column.Ref.Column,
	}
	if err := e.Type.UnmarshalType(column.Ref.Type); err != nil {
		return err
	}
	return nil
}
