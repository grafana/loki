package logical

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The FunctionOp instruction yields the result of function operation Op Value.
// UnaryOp implements both [Instruction] and [Value].
type FunctionOp struct {
	b baseNode

	Op     types.VariadicOp
	Values []Value
}

var (
	_ Value       = (*FunctionOp)(nil)
	_ Instruction = (*FunctionOp)(nil)
)

// Name returns an identifier for the UnaryOp operation.
func (u *FunctionOp) Name() string { return u.b.Name() }

// String returns the disassembled SSA form of the FunctionOp instruction.
func (u *FunctionOp) String() string {
	values := make([]string, len(u.Values))
	for i, v := range u.Values {
		values[i] = v.String()
	}
	return fmt.Sprintf("%s(%s)", u.Op, strings.Join(values, ", "))
}

// Operands appends the operands of u to the provided slice. The pointers may
// be modified to change operands of u.
func (u *FunctionOp) Operands(buf []*Value) []*Value {
	for i := range u.Values {
		buf = append(buf, &u.Values[i])
	}
	return buf
}

// Referrers returns a list of instructions that reference the FunctionOp.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (u *FunctionOp) Referrers() *[]Instruction { return &u.b.referrers }

func (u *FunctionOp) base() *baseNode { return &u.b }
func (u *FunctionOp) isValue()        {}
func (u *FunctionOp) isInstruction()  {}
