package logical

import (
	"fmt"

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

// IsNull returns true if lit is a [ValueTypeNull] value.
func (l Literal) IsNull() bool {
	return l.Kind() == types.ValueTypeNull
}

// Int64 returns lit's value as an int64. It panics if lit is not a
// [ValueTypeFloat].
func (l Literal) Float() float64 {
	if expect, actual := types.ValueTypeFloat, l.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.val.Value.(float64)
}

// Int returns lit's value as an int64. It panics if lit is not a
// [ValueTypeInt].
func (l Literal) Int() int64 {
	if expect, actual := types.ValueTypeInt, l.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.val.Value.(int64)
}

// Timestamp returns lit's value as a uint64. It panics if lit is not a
// [ValueTypeTimestamp].
func (l Literal) Timestamp() uint64 {
	if expect, actual := types.ValueTypeTimestamp, l.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.val.Value.(uint64)
}

// ByteArray returns lit's value as a byte slice. It panics if lit is not a
// [ValueTypeByteArray].
func (l Literal) ByteArray() []byte {
	if expect, actual := types.ValueTypeByteArray, l.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.val.Value.([]byte)
}

func (l *Literal) Schema() *schema.Schema {
	// TODO(rfratto): schema.Schema needs to be updated to be a more general
	// "type" instead.
	return nil
}

func (l *Literal) isValue() {}
