package types

import "fmt"

// UnaryOp denotes the kind of [UnaryOp] operation to perform.
type UnaryOp uint32

// Recognized values of [UnaryOp].
const (
	// UnaryOpKindInvalid indicates an invalid unary operation.
	UnaryOpInvalid UnaryOp = iota

	UnaryOpNot // Logical NOT operation (!).
	UnaryOpAbs // Mathematical absolute operation (abs).
)

// String returns the string representation of the UnaryOp.
func (t UnaryOp) String() string {
	switch t {
	case UnaryOpInvalid:
		return typeInvalid
	case UnaryOpNot:
		return "NOT"
	case UnaryOpAbs:
		return "ABS"
	default:
		panic(fmt.Sprintf("unknown unary operator %d", t))
	}
}

// BinaryOp denotes the kind of [BinaryOp] operation to perform.
type BinaryOp uint32

// Recognized values of [BinaryOp].
const (
	// BinaryOpInvalid indicates an invalid binary operation.
	BinaryOpInvalid BinaryOp = iota

	BinaryOpEq  // Equality comparison (==).
	BinaryOpNeq // Inequality comparison (!=).
	BinaryOpGt  // Greater than comparison (>).
	BinaryOpGte // Greater than or equal comparison (>=).
	BinaryOpLt  // Less than comparison (<).
	BinaryOpLte // Less than or equal comparison (<=).
	BinaryOpAnd // Logical AND operation (&&).
	BinaryOpOr  // Logical OR operation (||).
	BinaryOpXor // Logical XOR operation (^).
	BinaryOpNot // Logical NOT operation (!).

	BinaryOpAdd // Addition operation (+).
	BinaryOpSub // Subtraction operation (-).
	BinaryOpMul // Multiplication operation (*).
	BinaryOpDiv // Division operation (/).
	BinaryOpMod // Modulo operation (%).

	BinaryOpMatchStr    // String matching operation (|=).
	BinaryOpNotMatchStr // String non-matching operation (!=).
	BinaryOpMatchRe     // Regular expression matching operation (|~).
	BinaryOpNotMatchRe  // Regular expression non-matching operation (!~).
)

// String returns a human-readable representation of the binary operation kind.
func (t BinaryOp) String() string {
	switch t {
	case BinaryOpInvalid:
		return typeInvalid
	case BinaryOpEq:
		return "EQ"
	case BinaryOpNeq:
		return "NEQ" // convenience for NOT(EQ(expr))
	case BinaryOpGt:
		return "GT"
	case BinaryOpGte:
		return "GTE"
	case BinaryOpLt:
		return "LT" // convenience for NOT(GTE(expr))
	case BinaryOpLte:
		return "LTE" // convenience for NOT(GT(expr))
	case BinaryOpAnd:
		return "AND"
	case BinaryOpOr:
		return "OR"
	case BinaryOpXor:
		return "XOR"
	case BinaryOpNot:
		return "NOT"
	case BinaryOpAdd:
		return "ADD"
	case BinaryOpSub:
		return "SUB"
	case BinaryOpMul:
		return "MUL"
	case BinaryOpDiv:
		return "DIV"
	case BinaryOpMod:
		return "MOD"
	case BinaryOpMatchStr:
		return "MATCH_STR"
	case BinaryOpNotMatchStr:
		return "NOT_MATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BinaryOpMatchRe:
		return "MATCH_RE"
	case BinaryOpNotMatchRe:
		return "NOT_MATCH_RE" // convenience for NOT(MATCH_RE(...))
	default:
		panic(fmt.Sprintf("unknown binary operator %d", t))
	}
}
