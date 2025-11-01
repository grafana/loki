package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type literalUnmarshaler interface {
	UnmarshalLiteral(literal types.Literal) error
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_NullLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.NullLiteral = new(NullLiteral)
	return e.NullLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_BoolLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.BoolLiteral = new(BoolLiteral)
	return e.BoolLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_StringLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.StringLiteral = new(StringLiteral)
	return e.StringLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_IntegerLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.IntegerLiteral = new(IntegerLiteral)
	return e.IntegerLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_FloatLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.FloatLiteral = new(FloatLiteral)
	return e.FloatLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_TimestampLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.TimestampLiteral = new(TimestampLiteral)
	return e.TimestampLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_DurationLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.DurationLiteral = new(DurationLiteral)
	return e.DurationLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_BytesLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.BytesLiteral = new(BytesLiteral)
	return e.BytesLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into e. Returns an error if the conversion fails
// or is unsupported.
func (e *LiteralExpression_StringListLiteral) UnmarshalLiteral(literal types.Literal) error {
	e.StringListLiteral = new(StringListLiteral)
	return e.StringListLiteral.UnmarshalLiteral(literal)
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *NullLiteral) UnmarshalLiteral(literal types.Literal) error {
	_, ok := literal.(types.NullLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = NullLiteral{}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *BoolLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.BoolLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = BoolLiteral{Value: val.Value()}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *StringLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.StringLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = StringLiteral{Value: val.Value()}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *IntegerLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.IntegerLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = IntegerLiteral{Value: val.Value()}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *FloatLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.FloatLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = FloatLiteral{Value: val.Value()}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *TimestampLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.TimestampLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = TimestampLiteral{Value: int64(val.Value())}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *DurationLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.DurationLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = DurationLiteral{Value: int64(val.Value())}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *BytesLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.BytesLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = BytesLiteral{Value: int64(val.Value())}
	return nil
}

// UnmarshalLiteral reads from literal into l. Returns an error if the conversion fails
// or is unsupported.
func (l *StringListLiteral) UnmarshalLiteral(literal types.Literal) error {
	val, ok := literal.(types.StringListLiteral)
	if !ok {
		return fmt.Errorf("unsupported literal type: %T", literal)
	}

	*l = StringListLiteral{Value: val.Value()}
	return nil
}
