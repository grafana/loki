package logical

import "fmt"

// The Return instruction yields a value to return from a plan. Return
// implements [Instruction].
type Return struct {
	b baseNode

	Value Value // The value to return.
}

// String returns the disassembled SSA form of r.
func (r *Return) String() string {
	return fmt.Sprintf("RETURN %s", r.Value.Name())
}

// Operands appends the operands of r to the provided slice. The pointers may
// be modified to change operands of r.
func (r *Return) Operands(buf []*Value) []*Value {
	return append(buf, &r.Value)
}

func (r *Return) base() *baseNode { return &r.b }
func (r *Return) isInstruction()  {}
