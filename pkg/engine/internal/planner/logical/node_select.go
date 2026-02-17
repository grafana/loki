package logical

import (
	"fmt"
)

// The Select instruction filters rows from a table relation. Select implements
// both [Instruction] and [Value].
type Select struct {
	b baseNode

	Table Value // The table relation to filter.

	// Predicate is used to filter rows from Table. Each row is checked against
	// the given Predicate, and only rows for which the Predicate is true are
	// returned.
	Predicate Value
}

var (
	_ Value       = (*Select)(nil)
	_ Instruction = (*Select)(nil)
)

// Name returns an identifier for the Select operation.
func (s *Select) Name() string { return s.b.Name() }

// String returns the disassembled SSA form of the Select instruction.
func (s *Select) String() string {
	return fmt.Sprintf("SELECT %s [predicate=%s]", s.Table.Name(), s.Predicate.Name())
}

// Operands appends the operands of s to the provided slice. The pointers may
// be modified to change operands of s.
func (s *Select) Operands(buf []*Value) []*Value {
	return append(buf, &s.Table, &s.Predicate)
}

// Referrers returns a list of instructions that reference the Select.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (s *Select) Referrers() *[]Instruction { return &s.b.referrers }

func (s *Select) base() *baseNode { return &s.b }
func (s *Select) isInstruction()  {}
func (s *Select) isValue()        {}
