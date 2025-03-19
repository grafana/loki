package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// MakeTable represents a plan node that scans input data.
// It is the leaf node in the query tree, representing the initial data source
// from which all other operations will read. This is equivalent to a table scan
// operation in a relational database.
type MakeTable struct {
	// name is the identifier of the table to scan
	name string
	// schema defines the structure of the data in the table
	schema schema.Schema
}

// makeTable creates a new MakeTable plan node with the given name and schema.
// This is an internal constructor used by the public NewScan function.
func makeTable(name string, schema schema.Schema) *MakeTable {
	return &MakeTable{
		name:   name,
		schema: schema,
	}
}

// Schema returns the schema of the table.
// This implements part of the Plan interface.
func (t *MakeTable) Schema() schema.Schema {
	return t.schema
}

// TableSchema returns the schema of the table.
// This is a convenience method that returns the same value as Schema().
func (t *MakeTable) TableSchema() schema.Schema {
	return t.schema
}

// TableName returns the name of the table.
// This is used for identifying the data source in the query plan.
func (t *MakeTable) TableName() string {
	return t.name
}
