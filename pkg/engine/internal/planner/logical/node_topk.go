package logical

import (
	"fmt"
)

// TopK represents a plan node that performs topK operation.
// It sorts rows based on sort expressions and limits the result to the top K rows.
// This is equivalent to a SORT followed by a LIMIT operation.

// The TopK instruction sorts rows from a table relation. Sort implements both
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
