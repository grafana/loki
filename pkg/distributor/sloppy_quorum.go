package distributor

import (
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
)

// WriteExtend will return all nodes in the ring that are ACTIVE (healthy or not)
// We need this because Cortex `Write` operation won't select more than RF ACTIVE nodes, healthy or not.
// That's not good because non-healthy hosts will get filtered out and the writes can fail,
// even if there are enough nodes to satisfy RF
var WriteExtend = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, func(_ ring.InstanceState) bool {
	return true
})

// SloppyQuorumReplicationStrategy is a replication strategy that uses a sloppy quorum
// It prefers writing to the same nodes than the `DefaultReplicationStrategy`, but will write to other healthy nodes if some of the preferred are unhealthy, boosting availability
type SloppyQuorumReplicationStrategy struct{}

func NewSloppyQuorumReplicationStrategy() *ReplicationStrategy {
	return &ReplicationStrategy{
		Op:                  WriteExtend,
		ReplicationStrategy: &SloppyQuorumReplicationStrategy{},
	}
}

// Filter returns a quorum of healthy instances, or an error if that's not possible
func (r *SloppyQuorumReplicationStrategy) Filter(instances []ring.InstanceDesc, op ring.Operation, replicationFactor int, heartbeatTimeout time.Duration, zoneAwarenessEnabled bool) ([]ring.InstanceDesc, int, error) {
	now := time.Now()

	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout, now) {
			i++
		} else {
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	minSuccess := (replicationFactor / 2) + 1

	if len(instances) < minSuccess {
		var err error

		if zoneAwarenessEnabled {
			err = fmt.Errorf("at least %d healthy replicas required across different availability zones, could only find %d", minSuccess, len(instances))
		} else {
			err = fmt.Errorf("at least %d healthy replicas required, could only find %d", minSuccess, len(instances))
		}

		return nil, 0, err
	}

	if len(instances) > replicationFactor {
		instances = instances[:replicationFactor]
	}

	return instances, len(instances) - minSuccess, nil
}
