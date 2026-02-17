package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The UnaryOp instruction yields the result of unary operation Op Value.
// UnaryOp implements both [Instruction] and [Value].
type UnaryOp struct {
	b baseNode

	Op    types.UnaryOp
	Value Value
}

var (
	_ Value       = (*UnaryOp)(nil)
	_ Instruction = (*UnaryOp)(nil)
)

// Name returns an identifier for the UnaryOp operation.
func (u *UnaryOp) Name() string { return u.b.Name() }

// String returns the disassembled SSA form of the UnaryOp instruction.
func (u *UnaryOp) String() string {
	return fmt.Sprintf("%s(%s)", u.Op, u.Value.Name())
}

// Operands appends the operands of u to the provided slice. The pointers may
// be modified to change operands of u.
func (u *UnaryOp) Operands(buf []*Value) []*Value {
	return append(buf, &u.Value)
}

// Referrers returns a list of instructions that reference the UnaryOp.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (u *UnaryOp) Referrers() *[]Instruction { return &u.b.referrers }

func (u *UnaryOp) base() *baseNode { return &u.b }
func (u *UnaryOp) isValue()        {}
func (u *UnaryOp) isInstruction()  {}
