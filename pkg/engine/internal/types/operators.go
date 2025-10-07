package types //nolint:revive

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
	case UnaryOpNot:
		return "NOT"
	case UnaryOpAbs:
		return "ABS"
	default:
		panic(fmt.Sprintf("unknown unary operator %d", t))
	}
}

func GetUnwrapOp(op string) UnwrapOp {
	switch op {
	case "unwrap":
		return Unwrap
	case "unwrap bytes":
		return UnwrapBytes
	case "unwrap duration":
		return UnwrapDuration
	default:
		panic(fmt.Sprintf("unknown unwrap operation %s", op))
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

	BinaryOpMatchSubstr     // Substring matching operation (|=). Used for string match filter.
	BinaryOpNotMatchSubstr  // Substring non-matching operation (!=). Used for string match filter.
	BinaryOpMatchRe         // Regular expression matching operation (|~). Used for regex match filter and label matcher.
	BinaryOpNotMatchRe      // Regular expression non-matching operation (!~). Used for regex match filter and label matcher.
	BinaryOpMatchPattern    // Pattern matching operation (|>). Used for pattern match filter.
	BinaryOpNotMatchPattern // Pattern non-matching operation (!>). Use for pattern match filter.
)

// String returns a human-readable representation of the binary operation kind.
func (t BinaryOp) String() string {
	switch t {
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
	case BinaryOpMatchSubstr:
		return "MATCH_STR"
	case BinaryOpNotMatchSubstr:
		return "NOT_MATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BinaryOpMatchRe:
		return "MATCH_RE"
	case BinaryOpNotMatchRe:
		return "NOT_MATCH_RE" // convenience for NOT(MATCH_RE(...))
	case BinaryOpMatchPattern:
		return "MATCH_PAT"
	case BinaryOpNotMatchPattern:
		return "NOT_MATCH_PAT" // convenience for NOT(MATCH_PAT(...))
	default:
		panic(fmt.Sprintf("unknown binary operator %d", t))
	}
}

// UnwrapOp denotes the kind of [UnwrapOp] operation to perform.
type UnwrapOp uint32

// Recognized values of [UnwrapOp].
const (
	// UnwrapOpInvalid indicates an invalid unwrap operation.
	UnwrapOpInvalid UnwrapOp = iota
	Unwrap
	UnwrapBytes
	UnwrapDuration
	UnwrapDurationSeconds
)

func (t UnwrapOp) String() string {
	switch t {
	case Unwrap:
		return "unwrap"
	case UnwrapBytes:
		return "unwrap bytes"
	case UnwrapDuration:
		return "unwrap duration"
	default:
		panic(fmt.Sprintf("unknown unwrap operation %d", t))
	}
}
