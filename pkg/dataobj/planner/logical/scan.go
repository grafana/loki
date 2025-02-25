// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Scan implements Plan
var (
	_ Plan = &MakeTable{}
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

// Children returns the child plan nodes (none for Scan)
func (s *MakeTable) Children() []Plan {
	return nil
}

// Format implements format.Format
func (s *MakeTable) Format(fm format.Formatter) {
	n := format.Node{
		Singletons: []string{"Scan"},
		Tuples: []format.ContentTuple{{
			Key:   "name",
			Value: format.SingleContent(s.name),
		}},
	}

	fm.WriteNode(n)
}
