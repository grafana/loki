package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The MakeTable instruction yields a table relation from an identifier.
// MakeTable implements both [Instruction] and [Value].
type MakeTable struct {
	id string

	TableName   string        // Identifier of the table to scan.
	TableSchema schema.Schema // Structure of the data within the table.
}

var (
	_ Value       = (*MakeTable)(nil)
	_ Instruction = (*MakeTable)(nil)
)

// makeTable creates a new MakeTable plan node with the given name and schema.
// This is an internal constructor used by the public NewScan function.
func makeTable(name string, schema schema.Schema) *MakeTable {
	return &MakeTable{
		TableName:   name,
		TableSchema: schema,
	}
}

// Name returns an identifier for the MakeTable operation.
func (t *MakeTable) Name() string {
	if t.id != "" {
		return t.id
	}
	return fmt.Sprintf("<%p>", t)
}

// String returns the disassembled SSA form of the MakeTable instruction.
func (t *MakeTable) String() string {
	return fmt.Sprintf("make_table %s", t.TableName)
}

// Schema returns the schema of the table.
// This implements part of the Plan interface.
func (t *MakeTable) Schema() *schema.Schema {
	return &t.TableSchema
}

func (t *MakeTable) isInstruction() {}
func (t *MakeTable) isValue()       {}
