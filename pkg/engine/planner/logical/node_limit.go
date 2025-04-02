package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The Limit instruction limits the number of rows from a table relation. Limit
// implements [Instruction] and [Value].
type Limit struct {
	id string

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
func (l *Limit) Name() string {
	if l.id != "" {
		return l.id
	}
	return fmt.Sprintf("%p", l)
}

// String returns the disassembled SSA form of the Limit instruction.
func (l *Limit) String() string {
	// TODO(rfratto): change the type of l.Input to [Value] so we can use
	// s.Value.Name here.
	return fmt.Sprintf("LIMIT %v [skip=%d, fetch=%d]", l.Table.Name(), l.Skip, l.Fetch)
}

// Schema returns the schema of the limit operation.
func (l *Limit) Schema() *schema.Schema {
	// The schema is the same as the input plan's schema since limiting
	// only affects the number of rows, not their structure.
	return l.Table.Schema()
}

func (l *Limit) isInstruction() {}
func (l *Limit) isValue()       {}
