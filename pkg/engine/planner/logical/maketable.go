package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// MakeTable represents a plan node that scans input data.
// It is the leaf node in the query tree, representing the initial data source
// from which all other operations will read. This is equivalent to a table scan
// operation in a relational database.
type MakeTable struct {
	Name        string        // Identifier of the table to scan.
	TableSchema schema.Schema // Structure of the data within the table.
}

// makeTable creates a new MakeTable plan node with the given name and schema.
// This is an internal constructor used by the public NewScan function.
func makeTable(name string, schema schema.Schema) *MakeTable {
	return &MakeTable{
		Name:        name,
		TableSchema: schema,
	}
}

// Schema returns the schema of the table.
// This implements part of the Plan interface.
func (t *MakeTable) Schema() schema.Schema {
	return t.TableSchema
}
