package types

import "fmt"

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

// String returns the string representation of the LiteralKind.
func (k LiteralKind) String() string {
	switch k {
	case LiteralKindInvalid:
		return typeInvalid
	case LiteralKindNull:
		return "null"
	case LiteralKindString:
		return "string"
	case LiteralKindInt64:
		return "int64"
	case LiteralKindUint64:
		return "uint64"
	case LiteralKindByteArray:
		return "[]byte"
	default:
		return fmt.Sprintf("LiteralKind(%d)", k)
	}
}
