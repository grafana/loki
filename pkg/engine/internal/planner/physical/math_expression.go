package physical

import (
	"fmt"
)

// MathExpression represents an arithmetic operation
type MathExpression struct {
	id string

	// Expression is a math expression (a tree of BinOps or UnaryOps) with literals and a column reference as input.
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

// Clone returns a deep copy of the node (minus its ID).
func (m *MathExpression) Clone() Node {
	return &MathExpression{
		Expression: m.Expression.Clone(),
	}
}
