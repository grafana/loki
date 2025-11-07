package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	types.Literal
}

var _ Value = (*Literal)(nil)

func NewLiteral(value types.LiteralType) *Literal {
	if value == nil {
		return &Literal{Literal: types.NewNullLiteral()}
	}
	return &Literal{Literal: types.NewLiteral(value)}
}

// Kind returns the kind of value represented by the literal.
func (l Literal) Kind() types.DataType {
	return l.Type()
}

// Name returns the string form of the literal.
func (l Literal) Name() string {
	return l.Literal.String()
}

// String returns a printable form of the literal, even if lit is not a
// [ValueTypeString].
func (l Literal) String() string {
	return l.Literal.String()
}

// Value returns lit's value as untyped interface{}.
func (l Literal) Value() any {
	return l.Any()
}

func (l *Literal) isValue() {}
