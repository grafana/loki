package physical

import (
	"context"
	"slices"

	"github.com/oklog/ulid/v2"
)

// SortObject rewrites one complete logs object into a target sort schema and
// builds a replacement index over the rewritten object. It preserves the
// object's tenant and record contents and always produces one logs object.
type SortObject struct {
	NodeID ulid.ULID

	// SourceObjectPath is the object-storage path of the logs object to rewrite.
	SourceObjectPath string

	// SortSchema is applied to every tenant in the source object.
	SortSchema []string
}

// ID implements the Node interface.
func (n *SortObject) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*SortObject) Type() NodeType { return NodeTypeSortObject }

// Clone implements the Node interface.
func (n *SortObject) Clone() Node {
	return &SortObject{
		NodeID:           ulid.Make(),
		SourceObjectPath: n.SourceObjectPath,
		SortSchema:       slices.Clone(n.SortSchema),
	}
}

// CacheKey implements the Node interface. SortObject writes object-storage
// artifacts and is therefore not cacheable.
func (*SortObject) CacheKey(context.Context) string { return "" }
