package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

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

// The UnaryOp instruction yields the result of unary operation Op Value.
// UnaryOp implements both [Instruction] and [Value].
type UnaryOp struct {
	id string

	Op    UnaryOpKind
	Value Value
}

var (
	_ Value       = (*UnaryOp)(nil)
	_ Instruction = (*UnaryOp)(nil)
)

// Name returns an identifier for the UnaryOp operation.
func (u *UnaryOp) Name() string {
	if u.id != "" {
		return u.id
	}
	return fmt.Sprintf("<%p>", u)
}

// String returns the disassembled SSA form of the UnaryOp instruction.
func (u *UnaryOp) String() string {
	return fmt.Sprintf("%s %s", u.Op, u.Value.Name())
}

// Schema returns the schema of the UnaryOp plan.
func (u *UnaryOp) Schema() *schema.Schema {
	// TODO(rfratto): What should be returned here? Should the schema of BinOp
	// take on the schema of its Value? Does it depend on the operation?
	return nil
}

func (u *UnaryOp) isValue()       {}
func (u *UnaryOp) isInstruction() {}
