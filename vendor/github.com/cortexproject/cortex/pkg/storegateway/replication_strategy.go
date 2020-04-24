package storegateway

import (
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
)

type BlocksReplicationStrategy struct{}

func (s *BlocksReplicationStrategy) Filter(instances []ring.IngesterDesc, op ring.Operation, replicationFactor int, heartbeatTimeout time.Duration) ([]ring.IngesterDesc, int, error) {
	// Filter out unhealthy instances.
	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout) {
			i++
		} else {
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	// For the store-gateway use case we need that a block is loaded at least on
	// 1 instance, no matter what is the replication factor set (no quorum logic).
	if len(instances) == 0 {
		return nil, 0, errors.New("no healthy store-gateway instance found for the replication set")
	}

	maxFailures := len(instances) - 1
	return instances, maxFailures, nil
}

func (s *BlocksReplicationStrategy) ShouldExtendReplicaSet(instance ring.IngesterDesc, op ring.Operation) bool {
	switch op {
	case ring.BlocksSync:
		// If the instance is JOINING or LEAVING we should extend the replica set:
		// - JOINING: the previous replica set should be kept while an instance is JOINING
		// - LEAVING: the instance is going to be decommissioned soon so we need to include
		//   		  another replica in the set
		return instance.GetState() == ring.JOINING || instance.GetState() == ring.LEAVING
	case ring.BlocksRead:
		return false
	default:
		return false
	}
}
