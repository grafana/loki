package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The BinOp instruction yields the result of binary operation Left Op Right.
// BinOp implements both [Instruction] and [Value].
type BinOp struct {
	id string

	Left, Right Value
	Op          types.BinaryOp
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
	return fmt.Sprintf("%p", b)
}

// String returns the disassembled SSA form of the BinOp instruction.
func (b *BinOp) String() string {
	return fmt.Sprintf("%s %s %s", b.Op, b.Left.Name(), b.Right.Name())
}

func (b *BinOp) isValue()       {}
func (b *BinOp) isInstruction() {}
