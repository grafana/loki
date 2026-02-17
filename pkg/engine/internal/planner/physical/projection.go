package physical

import "github.com/oklog/ulid/v2"

// Projection represents a column selection operation in the physical plan.
// It contains a list of columns (column expressions) that are later
// evaluated against the input columns to remove unnecessary colums from the
// intermediate result.
type Projection struct {
	NodeID ulid.ULID

	// Expressions is a set of column expressions that are used to drop not needed
	// columns that match the column expression, or to expand columns that result
	// from the expressions.
	Expressions []Expression

	All    bool // Marker for projecting all columns of input relation (similar to SQL `SELECT *`)
	Expand bool // Indicates that projected columns should be added to input relation
	Drop   bool // Indicates that projected columns should be dropped from input Relation
}

// ID implements the [Node] interface.
// Returns the ULID that uniquely identifies the node in the plan.
func (p *Projection) ID() ulid.ULID { return p.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (p *Projection) Clone() Node {
	return &Projection{
		NodeID: ulid.Make(),

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
