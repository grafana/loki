package ring

import (
	"fmt"
)

// replicationStrategy decides, given the set of ingesters eligible for a key,
// which ingesters you will try and write to and how many failures you will
// tolerate.
// - Filters out dead ingesters so the one doesn't even try to write to them.
// - Checks there is enough ingesters for an operation to succeed.
// The ingesters argument may be overwritten.
func (r *Ring) replicationStrategy(ingesters []IngesterDesc, op Operation) ([]IngesterDesc, int, error) {
	// We need a response from a quorum of ingesters, which is n/2 + 1.  In the
	// case of a node joining/leaving, the actual replica set might be bigger
	// than the replication factor, so use the bigger or the two.
	replicationFactor := r.cfg.ReplicationFactor
	if len(ingesters) > replicationFactor {
		replicationFactor = len(ingesters)
	}
	minSuccess := (replicationFactor / 2) + 1
	maxFailure := replicationFactor - minSuccess

	// Skip those that have not heartbeated in a while. NB these are still
	// included in the calculation of minSuccess, so if too many failed ingesters
	// will cause the whole write to fail.
	for i := 0; i < len(ingesters); {
		if r.IsHealthy(&ingesters[i], op) {
			i++
		} else {
			ingesters = append(ingesters[:i], ingesters[i+1:]...)
			maxFailure--
		}
	}

	// This is just a shortcut - if there are not minSuccess available ingesters,
	// after filtering out dead ones, don't even bother trying.
	if maxFailure < 0 || len(ingesters) < minSuccess {
		err := fmt.Errorf("at least %d live ingesters required, could only find %d",
			minSuccess, len(ingesters))
		return nil, 0, err
	}

	return ingesters, maxFailure, nil
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
