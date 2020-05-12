package ring

import (
	"fmt"
	"time"
)

type ReplicationStrategy interface {
	// Filter out unhealthy instances and checks if there're enough instances
	// for an operation to succeed. Returns an error if there are not enough
	// instances.
	Filter(instances []IngesterDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration) (healthy []IngesterDesc, maxFailures int, err error)

	// ShouldExtendReplicaSet returns true if given an instance that's going to be
	// added to the replica set, the replica set size should be extended by 1
	// more instance for the given operation.
	ShouldExtendReplicaSet(instance IngesterDesc, op Operation) bool
}

type DefaultReplicationStrategy struct{}

// Filter decides, given the set of ingesters eligible for a key,
// which ingesters you will try and write to and how many failures you will
// tolerate.
// - Filters out dead ingesters so the one doesn't even try to write to them.
// - Checks there is enough ingesters for an operation to succeed.
// The ingesters argument may be overwritten.
func (s *DefaultReplicationStrategy) Filter(ingesters []IngesterDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration) ([]IngesterDesc, int, error) {
	// We need a response from a quorum of ingesters, which is n/2 + 1.  In the
	// case of a node joining/leaving, the actual replica set might be bigger
	// than the replication factor, so use the bigger or the two.
	if len(ingesters) > replicationFactor {
		replicationFactor = len(ingesters)
	}

	minSuccess := (replicationFactor / 2) + 1

	// Skip those that have not heartbeated in a while. NB these are still
	// included in the calculation of minSuccess, so if too many failed ingesters
	// will cause the whole write to fail.
	for i := 0; i < len(ingesters); {
		if ingesters[i].IsHealthy(op, heartbeatTimeout) {
			i++
		} else {
			ingesters = append(ingesters[:i], ingesters[i+1:]...)
		}
	}

	// This is just a shortcut - if there are not minSuccess available ingesters,
	// after filtering out dead ones, don't even bother trying.
	if len(ingesters) < minSuccess {
		err := fmt.Errorf("at least %d live replicas required, could only find %d",
			minSuccess, len(ingesters))
		return nil, 0, err
	}

	return ingesters, len(ingesters) - minSuccess, nil
}

func (s *DefaultReplicationStrategy) ShouldExtendReplicaSet(ingester IngesterDesc, op Operation) bool {
	// We do not want to Write to Ingesters that are not ACTIVE, but we do want
	// to write the extra replica somewhere.  So we increase the size of the set
	// of replicas for the key. This means we have to also increase the
	// size of the replica set for read, but we can read from Leaving ingesters,
	// so don't skip it in this case.
	// NB dead ingester will be filtered later by DefaultReplicationStrategy.Filter().
	if op == Write && ingester.State != ACTIVE {
		return true
	} else if op == Read && (ingester.State != ACTIVE && ingester.State != LEAVING) {
		return true
	}

	return false
}

// IsHealthy checks whether an ingester appears to be alive and heartbeating
func (r *Ring) IsHealthy(ingester *IngesterDesc, op Operation) bool {
	return ingester.IsHealthy(op, r.cfg.HeartbeatTimeout)
}

// ReplicationFactor of the ring.
func (r *Ring) ReplicationFactor() int {
	return r.cfg.ReplicationFactor
}

// IngesterCount is number of ingesters in the ring
func (r *Ring) IngesterCount() int {
	r.mtx.Lock()
	c := len(r.ringDesc.Ingesters)
	r.mtx.Unlock()
	return c
}
