package physical

import "fmt"

// Limit represents a limiting operation in the physical plan that applies
// offset and limit to the result set. The offset specifies how many rows to
// skip before starting to return results, while limit specifies the maximum
// number of rows to return.
type Limit struct {
	id string

	// Skip specifies how many initial rows should be skipped.
	Skip uint32
	// Fetch specifies how many rows should be returned in total.
	Fetch uint32
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (l *Limit) ID() string {
	if l.id == "" {
		return fmt.Sprintf("%p", l)
	}
	return l.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Limit) Type() NodeType {
	return NodeTypeLimit
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (l *Limit) Accept(v Visitor) error {
	return v.VisitLimit(l)
}
