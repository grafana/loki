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
	id string

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
func (s *TopK) Name() string {
	if s.id != "" {
		return s.id
	}
	return fmt.Sprintf("%p", s)
}

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

func (s *TopK) isInstruction() {}
func (s *TopK) isValue()       {}
