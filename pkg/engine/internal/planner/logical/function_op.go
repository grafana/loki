package logical

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The FunctionOp instruction yields the result of function operation Op Value.
// UnaryOp implements both [Instruction] and [Value].
type FunctionOp struct {
	id string

	Op     types.VariadicOp
	Values []Value
}

var (
	_ Value       = (*FunctionOp)(nil)
	_ Instruction = (*FunctionOp)(nil)
)

// Name returns an identifier for the UnaryOp operation.
func (u *FunctionOp) Name() string {
	if u.id != "" {
		return u.id
	}
	return fmt.Sprintf("%p", u)
}

// String returns the disassembled SSA form of the FunctionOp instruction.
func (u *FunctionOp) String() string {
	values := make([]string, len(u.Values))
	for i, v := range u.Values {
		values[i] = v.String()
	}
	return fmt.Sprintf("%s(%s)", u.Op, strings.Join(values, ", "))
}

func (u *FunctionOp) isValue()       {}
func (u *FunctionOp) isInstruction() {}
