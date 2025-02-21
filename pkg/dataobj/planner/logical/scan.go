// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Scan implements Plan
var (
	_ Plan = &Scan{}
)

// Scan represents a plan node that scans input data from a DataSource
// It is the leaf node in our query tree
type Scan struct {
	// dataSource provides access to the data
	dataSource DataSource
	// projection specifies which columns to include, empty means all columns
	projection []string
	// schema is derived from dataSource and projection
	schema schema.Schema
}

// NewScan creates a new Scan plan node
func NewScan(dataSource DataSource, projection []string) *Scan {
	s := &Scan{
		dataSource: dataSource,
		projection: projection,
	}
	s.schema = s.deriveSchema()
	return s
}

// Schema returns the schema of the data produced by this scan
func (s *Scan) Schema() schema.Schema {
	return s.schema
}

// deriveSchema determines the output schema based on the projection
func (s *Scan) deriveSchema() schema.Schema {
	sourceSchema := s.dataSource.Schema()
	if len(s.projection) == 0 {
		return sourceSchema
	}

	return sourceSchema.Filter(s.projection)
}

// Children returns the child plan nodes (none for Scan)
func (s *Scan) Children() []Plan {
	return nil
}
