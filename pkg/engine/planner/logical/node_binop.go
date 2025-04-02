package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
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

// Schema returns the schema of the BinOp operation.
func (b *BinOp) Schema() *schema.Schema {
	// TODO(rfratto): What should be returned here? Should the schema of BinOp
	// take on the schema of its LHS or RHS? Does it depend on the operation?
	return nil
}

func (b *BinOp) isValue()       {}
func (b *BinOp) isInstruction() {}
