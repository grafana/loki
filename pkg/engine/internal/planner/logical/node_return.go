package logical

import "fmt"

// The Return instruction yields a value to return from a plan. Return
// implements [Instruction].
type Return struct {
	Value Value // The value to return.
}

// String returns the disassembled SSA form of r.
func (r *Return) String() string {
	return fmt.Sprintf("RETURN %s", r.Value.Name())
}

func (r *Return) isInstruction() {}
