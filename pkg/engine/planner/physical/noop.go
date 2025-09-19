package physical

import "fmt"

// NoOp represents a node in the physical plan that can be eliminated in the optimisation step.
type NoOp struct {
	id string
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (n *NoOp) ID() string {
	if n.id == "" {
		return fmt.Sprintf("%p", n)
	}
	return n.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*NoOp) Type() NodeType {
	return NodeTypeNoOp
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (n *NoOp) Accept(v Visitor) error {
	return v.VisitNoOp(n)
}
