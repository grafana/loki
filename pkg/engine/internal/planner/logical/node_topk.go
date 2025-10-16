package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/schema"
)

// TopK represents a logical plan node that performs top K operations.
// It sorts rows based on sort expressions and limits the result to the top K rows.
// This is equivalent to a SORT followed by a LIMIT operation.
type TopK struct {
	id string

	Table Value // The table relation to sort and limit.

	// The column to sort by.
	Column ColumnRef
	// Whether to sort in ascending order.
	Ascending bool
	// Controls whether NULLs appear first (true) or last (false).
	NullsFirst bool
	// Number of top rows to return.
	K int
}

var (
	_ Value       = (*TopK)(nil)
	_ Instruction = (*TopK)(nil)
)

// Name returns an identifier for the TopK operation.
func (t *TopK) Name() string {
	if t.id != "" {
		return t.id
	}
	return fmt.Sprintf("%p", t)
}

// String returns the disassembled SSA form of the TopK instruction.
func (t *TopK) String() string {
	return fmt.Sprintf(
		"TOPK %s [column=%s, asc=%t, nulls_first=%t, k=%d]",
		t.Table.Name(),
		t.Column.String(),
		t.Ascending,
		t.NullsFirst,
		t.K,
	)
}

// Schema returns the schema of the TopK plan.
func (t *TopK) Schema() *schema.Schema {
	// The schema is the same as the input plan's schema since TopK only
	// affects the order and number of rows, not their structure.
	return t.Table.Schema()
}

func (t *TopK) isInstruction() {}
func (t *TopK) isValue()       {}
