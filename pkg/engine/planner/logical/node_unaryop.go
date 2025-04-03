package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The UnaryOp instruction yields the result of unary operation Op Value.
// UnaryOp implements both [Instruction] and [Value].
type UnaryOp struct {
	id string

	Op    types.UnaryOp
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
	return fmt.Sprintf("%p", u)
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
