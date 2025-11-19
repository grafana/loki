package physical

import "github.com/oklog/ulid/v2"

// Join represents a join operation in the physical plan.
// For now it is only an inner join on `timestamp`. Will be expanded later.
type Join struct {
	NodeID ulid.ULID
}

// ID implements the [Node] interface.
// Returns the ULID that uniquely identifies the node in the plan.
func (f *Join) ID() ulid.ULID { return f.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (f *Join) Clone() Node {
	return &Join{
		NodeID: ulid.Make(),
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Join) Type() NodeType {
	return NodeTypeJoin
}
