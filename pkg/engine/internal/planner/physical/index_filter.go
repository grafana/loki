package physical

import (
	"github.com/oklog/ulid/v2"
)

// IndexFilter copies part of one tenant's index into a new index object. It
// keeps the postings and stats rows of the listed log objects and drops all
// other rows. It reads only the source index and never reads log objects. The
// source index is one index object or the index objects of one index run.
type IndexFilter struct {
	NodeID ulid.ULID

	// Tenant selects the index sections to copy. Sections of other tenants
	// are not copied.
	Tenant string

	// SourceIndexPaths are the object-storage paths of every index object of
	// the index to filter. A run splits postings across its index objects, so
	// a filter over part of a run loses postings.
	SourceIndexPaths []string

	// ObjectPaths are the log objects whose rows the new index keeps.
	ObjectPaths []string
}

// ID implements the Node interface.
func (n *IndexFilter) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*IndexFilter) Type() NodeType { return NodeTypeIndexFilter }
