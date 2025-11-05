package logical

import (
	"fmt"
)

// The Select instruction filters rows from a table relation. Select implements
// both [Instruction] and [Value].
type Select struct {
	id string

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
func (s *Select) Name() string {
	if s.id != "" {
		return s.id
	}
	return fmt.Sprintf("%p", s)
}

// String returns the disassembled SSA form of the Select instruction.
func (s *Select) String() string {
	return fmt.Sprintf("SELECT %s [predicate=%s]", s.Table.Name(), s.Predicate.Name())
}

func (s *Select) isInstruction() {}
func (s *Select) isValue()       {}
