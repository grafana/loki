package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
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

// Schema returns the schema of the Select plan.
func (s *Select) Schema() *schema.Schema {
	// Since Select only filters rows from a table, the schema is the same as the
	// input table relation.
	return s.Table.Schema()
}

func (s *Select) isInstruction() {}
func (s *Select) isValue()       {}
