package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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
	return fmt.Sprintf("%s(%s)", u.Op, u.Value.Name())
}

func (u *UnaryOp) isValue()       {}
func (u *UnaryOp) isInstruction() {}
