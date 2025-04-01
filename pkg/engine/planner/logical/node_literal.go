package logical

import (
	"fmt"
	"strconv"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A Literal represents a literal value known at plan time. Literal only
// implements [Value].
//
// The zero value of a Literal is a NULL value.
type Literal struct {
	val any
}

var _ Value = (*Literal)(nil)

// LiteralString creates a new Literal value from a string.
func LiteralString(v string) *Literal { return &Literal{val: v} }

// LiteralInt64 creates a new Literal value from a 64-bit integer.
func LiteralInt64(v int64) *Literal { return &Literal{val: v} }

// LiteralUint64 creates a new Literal value from a 64-bit unsigned integer.
func LiteralUint64(v uint64) *Literal { return &Literal{val: v} }

// LiteralByteArray creates a new Literal value from a byte slice.
func LiteralByteArray(v []byte) *Literal { return &Literal{val: v} }

// Kind returns the kind of value represented by the literal.
func (lit Literal) Kind() types.LiteralKind {
	switch lit.val.(type) {
	case nil:
		return types.LiteralKindNull
	case string:
		return types.LiteralKindString
	case int64:
		return types.LiteralKindInt64
	case uint64:
		return types.LiteralKindUint64
	case []byte:
		return types.LiteralKindByteArray
	default:
		return types.LiteralKindInvalid
	}
}

// Name returns the string form of the literal.
func (lit Literal) Name() string {
	return lit.String()
}

// String returns a printable form of the literal, even if lit is not a
// [LiteralKindString].
func (lit Literal) String() string {
	switch lit.Kind() {
	case types.LiteralKindNull:
		return "NULL"
	case types.LiteralKindString:
		return strconv.Quote(lit.val.(string))
	case types.LiteralKindInt64:
		return strconv.FormatInt(lit.Int64(), 10)
	case types.LiteralKindUint64:
		return strconv.FormatUint(lit.Uint64(), 10)
	case types.LiteralKindByteArray:
		return fmt.Sprintf("%v", lit.val)
	default:
		return fmt.Sprintf("Literal(%s)", lit.Kind())
	}
}

// Value returns lit's value as untyped interface{}.
func (lit Literal) Value() any {
	return lit.val
}

// IsNull returns true if lit is a [LiteralKindNull] value.
func (lit Literal) IsNull() bool {
	return lit.Kind() == types.LiteralKindNull
}

// Int64 returns lit's value as an int64. It panics if lit is not a
// [LiteralKindInt64].
func (lit Literal) Int64() int64 {
	if expect, actual := types.LiteralKindInt64, lit.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return lit.val.(int64)
}

// Uint64 returns lit's value as a uint64. It panics if lit is not a
// [LiteralKindUint64].
func (lit Literal) Uint64() uint64 {
	if expect, actual := types.LiteralKindUint64, lit.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return lit.val.(uint64)
}

// ByteArray returns lit's value as a byte slice. It panics if lit is not a
// [LiteralKindByteArray].
func (lit Literal) ByteArray() []byte {
	if expect, actual := types.LiteralKindByteArray, lit.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return lit.val.([]byte)
}

func (lit *Literal) Schema() *schema.Schema {
	// TODO(rfratto): schema.Schema needs to be updated to be a more general
	// "type" instead.
	return nil
}

func (lit *Literal) isValue() {}
