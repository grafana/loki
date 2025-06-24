package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The MakeTable instruction yields a table relation from an identifier.
// MakeTable implements both [Instruction] and [Value].
type MakeTable struct {
	id string

	// Selector is used to generate a table relation. All streams for which the
	// selector passes are included in the resulting table.
	//
	// It is invalid for Selector to include a [ColumnRef] that is not
	// [ColumnTypeBuiltin] or [ColumnTypeLabel].
	Selector Value

	// Shard is used to indicate that the table relation does not contain all data
	// of the relation but only a subset of it.
	// The Shard value must be of type [ShardRef].
	Shard Value
}

var (
	_ Value       = (*MakeTable)(nil)
	_ Instruction = (*MakeTable)(nil)
)

// Name returns an identifier for the MakeTable operation.
func (t *MakeTable) Name() string {
	if t.id != "" {
		return t.id
	}
	return fmt.Sprintf("%p", t)
}

// String returns the disassembled SSA form of the MakeTable instruction.
func (t *MakeTable) String() string {
	return fmt.Sprintf("MAKETABLE [selector=%s, shard=%s]", t.Selector.Name(), t.Shard.Name())
}

// Schema returns the schema of the table.
// This implements part of the Plan interface.
func (t *MakeTable) Schema() *schema.Schema {
	// TODO(rfratto): What should we return here? What's possible for the logical
	// planner to know about the selector at planning time?
	return nil
}

func (t *MakeTable) isInstruction() {}
func (t *MakeTable) isValue()       {}
