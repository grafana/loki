package physical

import (
	"fmt"
)

// TopK represents a physical plan node that performs top K operations.
// It sorts rows based on sort expressions and limits the result to the top K rows.
// This is equivalent to a SORT followed by a LIMIT operation.
type TopK struct {
	id string

	// The column to sort by.
	Column ColumnExpression
	// Whether to sort in ascending order.
	Ascending bool
	// Controls whether NULLs appear first (true) or last (false).
	NullsFirst bool
	// Number of top rows to return.
	K int
	// Maximum number of unused rows to retain before compacting.
	MaxUnused int
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (t *TopK) ID() string {
	if t.id == "" {
		return fmt.Sprintf("%p", t)
	}
	return t.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*TopK) Type() NodeType {
	return NodeTypeTopK
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (t *TopK) Accept(visitor Visitor) error {
	return visitor.VisitTopK(t)
}
