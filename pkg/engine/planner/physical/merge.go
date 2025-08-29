package physical

import "fmt"

// Merge represents a merge operation in the physical plan that merges
// N inputs to 1 output.
type Merge struct {
	id string
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (m *Merge) ID() string {
	if m.id == "" {
		return fmt.Sprintf("%p", m)
	}

	return m.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (m *Merge) Type() NodeType {
	return NodeTypeMerge
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (m *Merge) Accept(v Visitor) error {
	return v.VisitMerge(m)
}
