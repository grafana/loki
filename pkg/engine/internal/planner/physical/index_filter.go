package physical

import (
	"context"
	"slices"

	"github.com/oklog/ulid/v2"
)

// IndexFilter copies part of one tenant's index into a new index object. It
// keeps the postings and stats rows of the listed log objects and drops all
// other rows. It reads only the source index and never reads log objects.
type IndexFilter struct {
	NodeID ulid.ULID

	// Tenant selects the index sections to copy. Sections of other tenants
	// are not copied.
	Tenant string

	// SourceIndexPath is the object-storage path of the index to filter.
	SourceIndexPath string

	// ObjectPaths are the log objects whose rows the new index keeps.
	ObjectPaths []string
}

// ID implements the Node interface.
func (n *IndexFilter) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*IndexFilter) Type() NodeType { return NodeTypeIndexFilter }

// Clone implements the Node interface.
func (n *IndexFilter) Clone() Node {
	return &IndexFilter{
		NodeID:          ulid.Make(),
		Tenant:          n.Tenant,
		SourceIndexPath: n.SourceIndexPath,
		ObjectPaths:     slices.Clone(n.ObjectPaths),
	}
}

// CacheKey implements the Node interface. IndexFilter writes object-storage
// artifacts and is therefore not cacheable.
func (*IndexFilter) CacheKey(context.Context) string { return "" }
