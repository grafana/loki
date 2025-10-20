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

// BinaryOp denotes the kind of [BinaryOp] operation to perform.
type BinaryOp uint32

// Recognized values of [BinaryOp].
const (
	// BinaryOpInvalid indicates an invalid binary operation.
	BinaryOpInvalid BinaryOp = iota

	BINARY_OP_EQ  // Equality comparison (==).
	BINARY_OP_NEQ // Inequality comparison (!=).
	BINARY_OP_GT  // Greater than comparison (>).
	BINARY_OP_GTE // Greater than or equal comparison (>=).
	BINARY_OP_LT  // Less than comparison (<).
	BINARY_OP_LTE // Less than or equal comparison (<=).

	BINARY_OP_AND // Logical AND operation (&&).
	BINARY_OP_OR  // Logical OR operation (||).
	BINARY_OP_XOR // Logical XOR operation (^).
	BinaryOpNot   // Logical NOT operation (!).

	BINARY_OP_ADD // Addition operation (+).
	BinaryOpSub   // Subtraction operation (-).
	BinaryOpMul   // Multiplication operation (*).
	BinaryOpDiv   // Division operation (/).
	BinaryOpMod   // Modulo operation (%).

	BINARY_OP_MATCH_SUBSTR      // Substring matching operation (|=). Used for string match filter.
	BINARY_OP_NOT_MATCH_SUBSTR  // Substring non-matching operation (!=). Used for string match filter.
	BINARY_OP_MATCH_RE          // Regular expression matching operation (|~). Used for regex match filter and label matcher.
	BINARY_OP_NOT_MATCH_RE      // Regular expression non-matching operation (!~). Used for regex match filter and label matcher.
	BINARY_OP_MATCH_PATTERN     // Pattern matching operation (|>). Used for pattern match filter.
	BINARY_OP_NOT_MATCH_PATTERN // Pattern non-matching operation (!>). Use for pattern match filter.
)

// String returns a human-readable representation of the binary operation kind.
func (t BinaryOp) String() string {
	switch t {
	case BINARY_OP_EQ:
		return "EQ"
	case BINARY_OP_NEQ:
		return "NEQ" // convenience for NOT(EQ(expr))
	case BINARY_OP_GT:
		return "GT"
	case BINARY_OP_GTE:
		return "GTE"
	case BINARY_OP_LT:
		return "LT" // convenience for NOT(GTE(expr))
	case BINARY_OP_LTE:
		return "LTE" // convenience for NOT(GT(expr))
	case BINARY_OP_AND:
		return "AND"
	case BINARY_OP_OR:
		return "OR"
	case BINARY_OP_XOR:
		return "XOR"
	case BinaryOpNot:
		return "NOT"
	case BINARY_OP_ADD:
		return "ADD"
	case BinaryOpSub:
		return "SUB"
	case BinaryOpMul:
		return "MUL"
	case BinaryOpDiv:
		return "DIV"
	case BinaryOpMod:
		return "MOD"
	case BINARY_OP_MATCH_SUBSTR:
		return "MATCH_STR"
	case BINARY_OP_NOT_MATCH_SUBSTR:
		return "NOT_MATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BINARY_OP_MATCH_RE:
		return "MATCH_RE"
	case BINARY_OP_NOT_MATCH_RE:
		return "NOT_MATCH_RE" // convenience for NOT(MATCH_RE(...))
	case BINARY_OP_MATCH_PATTERN:
		return "MATCH_PAT"
	case BINARY_OP_NOT_MATCH_PATTERN:
		return "NOT_MATCH_PAT" // convenience for NOT(MATCH_PAT(...))
	default:
		panic(fmt.Sprintf("unknown binary operator %d", t))
	}
}
