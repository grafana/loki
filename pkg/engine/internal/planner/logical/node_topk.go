package logical

import (
	"fmt"
)

// TopK represents a plan node that performs topK operation.
// Topk only identifies which rows belong in the top K, but does not
// guarantee any specific ordering of those rows in the compacted output. Callers
// should sort the result if a specific order is required.

// The TopK instruction find the top K rows from a table relation. TopK implements both
// [Instruction] and [Value].
type TopK struct {
	b baseNode

	Table Value // The table relation to sort.

	SortBy     *ColumnRef // The column to sort by.
	Ascending  bool       // Whether to sort in ascending order.
	NullsFirst bool       // Controls whether NULLs appear first (true) or last (false).
	K          int        // Number of top rows to return.
}

var (
	_ Value       = (*TopK)(nil)
	_ Instruction = (*TopK)(nil)
)

// Name returns an identifier for the TopK operation.
func (s *TopK) Name() string { return s.b.Name() }

// String returns the disassembled SSA form of the TopK instruction.
func (s *TopK) String() string {
	return fmt.Sprintf(
		"TOPK %s [sort_by=%s, k=%d, asc=%t, nulls_first=%t]",
		s.Table.Name(),
		s.SortBy.String(),
		s.K,
		s.Ascending,
		s.NullsFirst,
	)
}

// Operands appends the operands of s to the provided slice. The pointers may
// be modified to change operands of s.
func (s *TopK) Operands(buf []*Value) []*Value {
	return append(buf, &s.Table)
}

// Referrers returns a list of instructions that reference the TopK.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (s *TopK) Referrers() *[]Instruction { return &s.b.referrers }

func (s *TopK) base() *baseNode { return &s.b }
func (s *TopK) isInstruction()  {}
func (s *TopK) isValue()        {}
