package physical

import (
	"github.com/oklog/ulid/v2"
)

// DeletionSweep applies delete requests to the objects of one tenant.
type DeletionSweep struct {
	NodeID ulid.ULID

	// Tenant selects the objects to sweep. It must not be empty. The sweep
	// does not touch objects of other tenants.
	Tenant string
}

// ID implements the Node interface.
func (n *DeletionSweep) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*DeletionSweep) Type() NodeType { return NodeTypeDeletionSweep }
