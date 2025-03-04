// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure MakeTable implements Plan and tableNode
var (
	_ Plan      = &MakeTable{}
	_ tableNode = &MakeTable{}
)

// MakeTable represents a plan node that scans input data from a DataSource
// It is the leaf node in our query tree
type MakeTable struct {
	// name is the identifier for this table
	name string
	// schema is the schema of the table
	schema schema.Schema
}

// NewScan creates a new Scan plan node
func NewScan(name string, schema schema.Schema) *MakeTable {
	return &MakeTable{
		name:   name,
		schema: schema,
	}
}

func (s *MakeTable) Schema() schema.Schema {
	return s.schema
}

// Category implements the Plan interface
func (s *MakeTable) Category() PlanCategory {
	return PlanCategoryTable
}

// TableSchema implements the tableNode interface
func (s *MakeTable) TableSchema() schema.Schema {
	return s.schema
}

// TableName implements the tableNode interface
func (s *MakeTable) TableName() string {
	return s.name
}
