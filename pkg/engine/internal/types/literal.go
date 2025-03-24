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
