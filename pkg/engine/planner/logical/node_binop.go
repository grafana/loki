package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

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

// The BinOp instruction yields the result of binary operation Left Op Right.
// BinOp implements both [Instruction] and [Value].
type BinOp struct {
	id string

	Left, Right Value
	Op          BinOpKind
}

var (
	_ Value       = (*BinOp)(nil)
	_ Instruction = (*BinOp)(nil)
)

// Name returns an identifier for the BinOp operation.
func (b *BinOp) Name() string {
	if b.id != "" {
		return b.id
	}
	return fmt.Sprintf("<%p>", b)
}

// String returns the disassembled SSA form of the BinOp instruction.
func (b *BinOp) String() string {
	return fmt.Sprintf("%s %s, %s", b.Op, b.Left.Name(), b.Right.Name())
}

// Schema returns the schema of the BinOp operation.
func (b *BinOp) Schema() *schema.Schema {
	// TODO(rfratto): What should be returned here? Should the schema of BinOp
	// take on the schema of its LHS or RHS? Does it depend on the operation?
	return nil
}

func (b *BinOp) isValue()       {}
func (b *BinOp) isInstruction() {}
