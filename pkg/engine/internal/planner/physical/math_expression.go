package physical

import (
	"fmt"
)

// MathExpression represents an arithmetic operation
type MathExpression struct {
	id string

	Expression Expression
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (m *MathExpression) ID() string {
	if m.id == "" {
		return fmt.Sprintf("%p", m)
	}
	return m.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*MathExpression) Type() NodeType {
	return NodeTypeMathExpression
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (m *MathExpression) Accept(v Visitor) error {
	return v.VisitMathExpression(m)
}
