package distributor

import "github.com/cortexproject/cortex/pkg/ring"

// ReplicationStrategy is a combination of operation and filter
type ReplicationStrategy struct {
	ring.ReplicationStrategy
	Op ring.Operation
}

// DefaultReplicationStrategy is the legacy strategy used by Loki
// It selects the first RF active nodes within `ring.ReadRing.Get()`` and removes unhealthy nodes using the default strategy filter
// When nodes > RF, it does not replace unhealthy nodes with healthy ones and writes can fail even if there is a quorum of healthy nodes for the write
var DefaultReplicationStrategy = &ReplicationStrategy{
	Op:                  ring.Write,
	ReplicationStrategy: ring.NewDefaultReplicationStrategy(),
}
