package logical

import (
	"fmt"
)

// Sort represents a plan node that sorts rows based on sort expressions.
// It corresponds to the ORDER BY clause in SQL and is used to order
// the results of a query based on one or more sort expressions.

// The Sort instruction sorts rows from a table relation. Sort implements both
// [Instruction] and [Value].
type Sort struct {
	b baseNode

	Table Value // The table relation to sort.

	Column     ColumnRef // The column to sort by.
	Ascending  bool      // Whether to sort in ascending order.
	NullsFirst bool      // Controls whether NULLs appear first (true) or last (false).
}

var (
	_ Value       = (*Sort)(nil)
	_ Instruction = (*Sort)(nil)
)

// Name returns an identifier for the Sort operation.
func (s *Sort) Name() string { return s.b.Name() }

// String returns the disassembled SSA form of the Sort instruction.
func (s *Sort) String() string {
	return fmt.Sprintf(
		"SORT %s [column=%s, asc=%t, nulls_first=%t]",
		s.Table.Name(),
		s.Column.String(),
		s.Ascending,
		s.NullsFirst,
	)
}

// Operands appends the operands of s to the provided slice. The pointers may
// be modified to change operands of s.
func (s *Sort) Operands(buf []*Value) []*Value {
	// NOTE(rfratto): Only fields of type Value are considered operands, so
	// r.Column is ignored here. Should that change?
	return append(buf, &s.Table)
}

// Referrers returns a list of instructions that reference the Sort.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (s *Sort) Referrers() *[]Instruction { return &s.b.referrers }

func (s *Sort) base() *baseNode { return &s.b }
func (s *Sort) isInstruction()  {}
func (s *Sort) isValue()        {}
