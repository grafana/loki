package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	b     baseNode
	inner types.Literal
}

var _ Value = (*Literal)(nil)

func NewLiteral(value any) *Literal {
	if value == nil {
		return &Literal{inner: types.NewNullLiteral()}
	}
	return &Literal{inner: types.NewLiteral(value)}
}

// Kind returns the kind of value represented by the literal.
func (l Literal) Kind() types.DataType {
	return l.inner.Type()
}

// Name returns the string form of the literal.
func (l Literal) Name() string {
	return l.inner.String()
}

// String returns a printable form of the literal, even if lit is not a
// [ValueTypeString].
func (l Literal) String() string {
	return l.inner.String()
}

// Value returns lit's value as untyped interface{}.
func (l Literal) Value() any {
	return l.inner.Any()
}

// Referrers returns a list of instructions that reference the Literal.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (l *Literal) Referrers() *[]Instruction { return &l.b.referrers }

func (l *Literal) base() *baseNode { return &l.b }
func (l *Literal) isValue()        {}
