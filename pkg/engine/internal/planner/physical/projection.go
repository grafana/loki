package physical

import "fmt"

// Projection represents a column selection operation in the physical plan.
// It contains a list of columns (column expressions) that are later
// evaluated against the input columns to remove unnecessary colums from the
// intermediate result.
type Projection struct {
	id string

	// Expressions is a set of column expressions that are used to drop not needed
	// columns that match the column expression, or to expand columns that result
	// from the expressions.
	Expressions []Expression

	All    bool // Marker for projecting all columns of input relation (similar to SQL `SELECT *`)
	Expand bool // Indicates that projected columns should be added to input relation
	Drop   bool // Indicates that projected columns should be dropped from input Relation
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (p *Projection) ID() string {
	if p.id == "" {
		return fmt.Sprintf("%p", p)
	}
	return p.id
}

// Clone returns a deep copy of the node (minus its ID).
func (p *Projection) Clone() Node {
	return &Projection{
		Expressions: cloneExpressions(p.Expressions),
		All:         p.All,
		Expand:      p.Expand,
		Drop:        p.Drop,
	}
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
