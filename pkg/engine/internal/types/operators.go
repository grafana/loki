package types

import "fmt"

// UnaryOpKind denotes the kind of [UnaryOp] operation to perform.
type UnaryOpKind int

// Recognized values of [UnaryOpKind].
const (
	// UnaryOpKindInvalid indicates an invalid unary operation.
	UnaryOpKindInvalid UnaryOpKind = iota

	UnaryOpKindNot // Logical NOT operation (!).
)

var unaryOpKindStrings = map[UnaryOpKind]string{
	UnaryOpKindInvalid: "invalid",

	UnaryOpKindNot: "NOT",
}

// String returns the string representation of the UnaryOpKind.
func (k UnaryOpKind) String() string {
	if s, ok := unaryOpKindStrings[k]; ok {
		return s
	}
	return fmt.Sprintf("UnaryOpKind(%d)", k)
}

// BinOpKind denotes the kind of [BinOp] operation to perform.
type BinOpKind int

// Recognized values of [BinOpKind].
const (
	// BinOpKindInvalid indicates an invalid binary operation.
	BinOpKindInvalid BinOpKind = iota

	BinOpKindEq  // Equality comparison (==).
	BinOpKindNeq // Inequality comparison (!=).
	BinOpKindGt  // Greater than comparison (>).
	BinOpKindGte // Greater than or equal comparison (>=).
	BinOpKindLt  // Less than comparison (<).
	BinOpKindLte // Less than or equal comparison (<=).
	BinOpKindAnd // Logical AND operation (&&).
	BinOpKindOr  // Logical OR operation (||).
	BinOpKindXor // Logical XOR operation (^).
	BinOpKindNot // Logical NOT operation (!).

	BinOpKindAdd // Addition operation (+).
	BinOpKindSub // Subtraction operation (-).
	BinOpKindMul // Multiplication operation (*).
	BinOpKindDiv // Division operation (/).
	BinOpKindMod // Modulo operation (%).

	BinOpKindMatchStr    // String matching operation.
	BinOpKindNotMatchStr // String non-matching operation.
	BinOpKindMatchRe     // Regular expression matching operation.
	BinOpKindNotMatchRe  // Regular expression non-matching operation.
)

var binOpKindStrings = map[BinOpKind]string{
	BinOpKindInvalid: "invalid",

	BinOpKindEq:  "EQ",
	BinOpKindNeq: "NEQ",
	BinOpKindGt:  "GT",
	BinOpKindGte: "GTE",
	BinOpKindLt:  "LT",
	BinOpKindLte: "LTE",
	BinOpKindAnd: "AND",
	BinOpKindOr:  "OR",
	BinOpKindXor: "XOR",
	BinOpKindNot: "NOT",

	BinOpKindAdd: "ADD",
	BinOpKindSub: "SUB",
	BinOpKindMul: "MUL",
	BinOpKindDiv: "DIV",
	BinOpKindMod: "MOD",

	BinOpKindMatchStr:    "MATCH_STR",
	BinOpKindNotMatchStr: "NOT_MATCH_STR",
	BinOpKindMatchRe:     "MATCH_RE",
	BinOpKindNotMatchRe:  "NOT_MATCH_RE",
}

// String returns a human-readable representation of the binary operation kind.
func (k BinOpKind) String() string {
	if s, ok := binOpKindStrings[k]; ok {
		return s
	}
	return fmt.Sprintf("BinOpKind(%d)", k)
}
