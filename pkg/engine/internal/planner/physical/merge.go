package physical

import "github.com/oklog/ulid/v2"

// Merge combines multiple input streams into a single stream with no
// guaranteed ordering.
//
// Merge is primarily used as an aggregation point for distributed task
// execution where a parent task needs to read from many partitioned tasks.
type Merge struct {
	NodeID ulid.ULID
}

func (m *Merge) ID() ulid.ULID { return m.NodeID }

func (m *Merge) Clone() Node {
	return &Merge{
		NodeID: ulid.Make(),
	}
}

func (m *Merge) Type() NodeType { return NodeTypeMerge }
