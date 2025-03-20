package logical

import (
	"fmt"
	"strconv"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// LiteralKind denotes the kind of [Literal] value.
type LiteralKind int

// Recognized values of [LiteralKind].
const (
	// LiteralKindInvalid indicates an invalid literal value.
	LiteralKindInvalid LiteralKind = iota

	LiteralKindNull      // NULL literal value.
	LiteralKindString    // String literal value.
	LiteralKindInt64     // 64-bit integer literal value.
	LiteralKindUint64    // 64-bit unsigned integer literal value.
	LiteralKindByteArray // Byte array literal value.
)

var literalKindStrings = map[LiteralKind]string{
	LiteralKindInvalid: "invalid",

	LiteralKindNull:      "null",
	LiteralKindString:    "string",
	LiteralKindInt64:     "int64",
	LiteralKindUint64:    "uint64",
	LiteralKindByteArray: "[]byte",
}

// String returns the string representation of the LiteralKind.
func (k LiteralKind) String() string {
	if s, ok := literalKindStrings[k]; ok {
		return s
	}
	return fmt.Sprintf("LiteralKind(%d)", k)
}

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
func (lit Literal) Kind() LiteralKind {
	switch lit.val.(type) {
	case nil:
		return LiteralKindNull
	case string:
		return LiteralKindString
	case int64:
		return LiteralKindInt64
	case uint64:
		return LiteralKindUint64
	case []byte:
		return LiteralKindByteArray
	default:
		return LiteralKindInvalid
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
	case LiteralKindNull:
		return "NULL"
	case LiteralKindString:
		return strconv.Quote(lit.val.(string))
	case LiteralKindInt64:
		return strconv.FormatInt(lit.Int64(), 10)
	case LiteralKindUint64:
		return strconv.FormatUint(lit.Uint64(), 10)
	case LiteralKindByteArray:
		return fmt.Sprintf("%v", lit.val)
	default:
		return fmt.Sprintf("Literal(%s)", lit.Kind())
	}
}

// IsNull returns true if lit is a [LiteralKindNull] value.
func (lit Literal) IsNull() bool {
	return lit.Kind() == LiteralKindNull
}

// Int64 returns lit's value as an int64. It panics if lit is not a
// [LiteralKindInt64].
func (lit Literal) Int64() int64 {
	if expect, actual := LiteralKindInt64, lit.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return lit.val.(int64)
}

// Uint64 returns lit's value as a uint64. It panics if lit is not a
// [LiteralKindUint64].
func (lit Literal) Uint64() uint64 {
	if expect, actual := LiteralKindUint64, lit.Kind(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return lit.val.(uint64)
}

// ByteArray returns lit's value as a byte slice. It panics if lit is not a
// [LiteralKindByteArray].
func (lit Literal) ByteArray() []byte {
	if expect, actual := LiteralKindByteArray, lit.Kind(); expect != actual {
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
