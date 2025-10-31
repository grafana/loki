package physical

import "fmt"

// Join represents a join operation in the physical plan.
// For now it is only an inner join on `timestamp`. Will be expanded later.
type Join struct {
	id string
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (f *Join) ID() string {
	if f.id == "" {
		return fmt.Sprintf("%p", f)
	}
	return f.id
}

// Clone returns a deep copy of the node (minus its ID).
func (f *Join) Clone() Node {
	return &Join{}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Join) Type() NodeType {
	return NodeTypeJoin
}
