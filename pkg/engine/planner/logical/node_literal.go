package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	val types.Literal
}

var _ Value = (*Literal)(nil)

func NewLiteral[T any](v T) *Literal {
	return &Literal{
		val: types.Literal{Value: v},
	}
}

// Kind returns the kind of value represented by the literal.
func (l Literal) Kind() types.ValueType {
	return l.val.ValueType()
}

// Name returns the string form of the literal.
func (l Literal) Name() string {
	return l.val.String()
}

// String returns a printable form of the literal, even if lit is not a
// [ValueTypeString].
func (l Literal) String() string {
	return l.val.String()
}

// Value returns lit's value as untyped interface{}.
func (l Literal) Value() any {
	return l.val.Value
}

func (l *Literal) Schema() *schema.Schema {
	// TODO(rfratto): schema.Schema needs to be updated to be a more general
	// "type" instead.
	return nil
}

func (l *Literal) isValue() {}
