package physical

import "github.com/oklog/ulid/v2"

// Parallelize represents a hint to the engine to partition and parallelize the
// children branches of the Parallelize and emit results as a single sequence
// with no guaranteed order.
type Parallelize struct {
	NodeID ulid.ULID
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (p *Parallelize) ID() ulid.ULID { return p.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (p *Parallelize) Clone() Node {
	return &Parallelize{
		NodeID: ulid.Make(),
	}
}

// Type returns [NodeTypeParallelize].
func (p *Parallelize) Type() NodeType { return NodeTypeParallelize }
