package physical

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Projection represents a column selection operation in the physical plan.
// It contains a list of columns (column expressions) that are later
// evaluated against the input columns to remove unnecessary colums from the
// intermediate result.
type Projection struct {
	id string

	// Columns is a set of column expressions that are used to drop not needed
	// columns that do not match the expression evaluation.
	Columns []ColumnExpression

	// Functions produce new records with additional columns by applying a function to a record and returning a new record.
	Functions []ProjectionFunction
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (p *Projection) ID() string {
	if p.id == "" {
		return fmt.Sprintf("%p", p)
	}
	return p.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Projection) Type() NodeType {
	return NodeTypeProjection
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (p *Projection) Accept(v Visitor) error {
	return v.VisitProjection(p)
}

type ProjectionFunction func(arrow.Record, memory.Allocator) (arrow.Record, error)
