package ruler

import (
	"time"

	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/ring"
)

type rulerReplicationStrategy struct {
}

func (r rulerReplicationStrategy) Filter(instances []ring.IngesterDesc, op ring.Operation, _ int, heartbeatTimeout time.Duration, _ bool) (healthy []ring.IngesterDesc, maxFailures int, err error) {
	// Filter out unhealthy instances.
	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout) {
			i++
		} else {
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	if len(instances) == 0 {
		return nil, 0, errors.New("no healthy ruler instance found for the replication set")
	}

	return instances, len(instances) - 1, nil
}

func (r rulerReplicationStrategy) ShouldExtendReplicaSet(instance ring.IngesterDesc, op ring.Operation) bool {
	// Only ACTIVE rulers get any rule groups. If instance is not ACTIVE, we need to find another ruler.
	if op == ring.Ruler && instance.GetState() != ring.ACTIVE {
		return true
	}
	return false
}
