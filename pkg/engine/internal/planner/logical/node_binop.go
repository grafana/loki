package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The BinOp instruction yields the result of binary operation Left Op Right.
// BinOp implements both [Instruction] and [Value].
type BinOp struct {
	b baseNode

	Left, Right Value
	Op          types.BinaryOp
}

var (
	_ Value       = (*BinOp)(nil)
	_ Instruction = (*BinOp)(nil)
)

// Name returns an identifier for the BinOp operation.
func (b *BinOp) Name() string { return b.b.Name() }

// String returns the disassembled SSA form of the BinOp instruction.
func (b *BinOp) String() string {
	return fmt.Sprintf("%s %s %s", b.Op, b.Left.Name(), b.Right.Name())
}

// Operands appends the operands of b to the provided slice. The pointers may
// be modified to change operands of b.
func (b *BinOp) Operands(buf []*Value) []*Value {
	return append(buf, &b.Left, &b.Right)
}

// Referrers returns a list of instructions that reference the BinOp.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (b *BinOp) Referrers() *[]Instruction { return &b.b.referrers }

func (b *BinOp) base() *baseNode { return &b.b }
func (b *BinOp) isValue()        {}
func (b *BinOp) isInstruction()  {}
