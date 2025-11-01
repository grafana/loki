package physical

import "github.com/oklog/ulid/v2"

// TopK represents a physical plan node that performs topK operation.
// It sorts rows based on sort expressions and limits the result to the top K rows.
// This is equivalent to a SORT followed by a LIMIT operation.
type TopK struct {
	NodeID ulid.ULID

	// SortBy is the column to sort by.
	SortBy     ColumnExpression
	Ascending  bool // Sort lines in ascending order if true.
	NullsFirst bool // When true, considers NULLs < non-NULLs when sorting.
	K          int  // Number of top rows to return.
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (t *TopK) ID() string { return t.NodeID.String() }

// Clone returns a deep copy of the node (minus its ID).
func (t *TopK) Clone() Node {
	return &TopK{
		SortBy:     t.SortBy.Clone().(ColumnExpression),
		Ascending:  t.Ascending,
		NullsFirst: t.NullsFirst,
		K:          t.K,
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*TopK) Type() NodeType {
	return NodeTypeTopK
}
