package logical

import (
	"fmt"
)

// The Limit instruction limits the number of rows from a table relation. Limit
// implements [Instruction] and [Value].
type Limit struct {
	b baseNode

	Table Value // Table relation to limit.

	// Skip is the number of rows to skip before returning results. A value of 0
	// means no rows are skipped.
	Skip uint32

	// Fetch is the maximum number of rows to return. A value of 0 means all rows
	// are returned (after applying Skip).
	Fetch uint32
}

var (
	_ Value       = (*Limit)(nil)
	_ Instruction = (*Limit)(nil)
)

// Name returns an identifier for the Limit operation.
func (l *Limit) Name() string { return l.b.Name() }

// String returns the disassembled SSA form of the Limit instruction.
func (l *Limit) String() string {
	// TODO(rfratto): change the type of l.Input to [Value] so we can use
	// s.Value.Name here.
	return fmt.Sprintf("LIMIT %v [skip=%d, fetch=%d]", l.Table.Name(), l.Skip, l.Fetch)
}

// Operands appends the operands of l to the provided slice. The pointers may
// be modified to change operands of l.
func (l *Limit) Operands(buf []*Value) []*Value {
	return append(buf, &l.Table)
}

// Referrers returns a list of instructions that reference the Limit.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (l *Limit) Referrers() *[]Instruction { return &l.b.referrers }

func (l *Limit) base() *baseNode { return &l.b }
func (l *Limit) isInstruction()  {}
func (l *Limit) isValue()        {}
