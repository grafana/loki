package expressionpb

import "github.com/grafana/loki/v3/pkg/engine/internal/types"

type literalMarshaler interface {
	MarshalLiteral() (types.Literal, error)
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_NullLiteral) MarshalLiteral() (types.Literal, error) {
	return e.NullLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_BoolLiteral) MarshalLiteral() (types.Literal, error) {
	return e.BoolLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_StringLiteral) MarshalLiteral() (types.Literal, error) {
	return e.StringLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_IntegerLiteral) MarshalLiteral() (types.Literal, error) {
	return e.IntegerLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_FloatLiteral) MarshalLiteral() (types.Literal, error) {
	return e.FloatLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_TimestampLiteral) MarshalLiteral() (types.Literal, error) {
	return e.TimestampLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_DurationLiteral) MarshalLiteral() (types.Literal, error) {
	return e.DurationLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_BytesLiteral) MarshalLiteral() (types.Literal, error) {
	return e.BytesLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (e *LiteralExpression_StringListLiteral) MarshalLiteral() (types.Literal, error) {
	return e.StringListLiteral.MarshalLiteral()
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *NullLiteral) MarshalLiteral() (types.Literal, error) {
	return types.NullLiteral{}, nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *BoolLiteral) MarshalLiteral() (types.Literal, error) {
	return types.BoolLiteral(l.Value), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *StringLiteral) MarshalLiteral() (types.Literal, error) {
	return types.StringLiteral(l.Value), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *IntegerLiteral) MarshalLiteral() (types.Literal, error) {
	return types.IntegerLiteral(l.Value), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *FloatLiteral) MarshalLiteral() (types.Literal, error) {
	return types.FloatLiteral(l.Value), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *TimestampLiteral) MarshalLiteral() (types.Literal, error) {
	return types.TimestampLiteral(types.Timestamp(l.Value)), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *DurationLiteral) MarshalLiteral() (types.Literal, error) {
	return types.DurationLiteral(types.Duration(l.Value)), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *BytesLiteral) MarshalLiteral() (types.Literal, error) {
	return types.BytesLiteral(types.Bytes(l.Value)), nil
}

// MarshalLiteral converts a protobuf literal into a types literal.
// Returns an error if the conversion fails or is unsupported.
func (l *StringListLiteral) MarshalLiteral() (types.Literal, error) {
	return types.StringListLiteral(l.Value), nil
}
