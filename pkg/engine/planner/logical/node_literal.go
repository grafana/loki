package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	datatype.Literal
}

var _ Value = (*Literal)(nil)

func NewLiteral(value datatype.LiteralType) *Literal {
	if value == nil {
		return &Literal{Literal: datatype.NewNullLiteral()}
	}
	return &Literal{Literal: datatype.NewLiteral(value)}
}

// Kind returns the kind of value represented by the literal.
func (l Literal) Kind() datatype.DataType {
	return l.Literal.Type()
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
	return l.Literal.Any()
}

func (l *Literal) Schema() *schema.Schema {
	// TODO(rfratto): schema.Schema needs to be updated to be a more general
	// "type" instead.
	return nil
}

func (l *Literal) isValue() {}
