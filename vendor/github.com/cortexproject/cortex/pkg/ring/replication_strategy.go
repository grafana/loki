package ring

import (
	"fmt"
	"time"
)

// replicationStrategy decides, given the set of ingesters eligible for a key,
// which ingesters you will try and write to and how many failures you will
// tolerate.
// - Filters out dead ingesters so the one doesn't even try to write to them.
// - Checks there is enough ingesters for an operation to succeed.
func (r *Ring) replicationStrategy(ingesters []IngesterDesc, op Operation) (
	liveIngesters []IngesterDesc, maxFailure int, err error,
) {
	// We need a response from a quorum of ingesters, which is n/2 + 1.  In the
	// case of a node joining/leaving, the actual replica set might be bigger
	// than the replication factor, so use the bigger or the two.
	replicationFactor := r.cfg.ReplicationFactor
	if len(ingesters) > replicationFactor {
		replicationFactor = len(ingesters)
	}
	minSuccess := (replicationFactor / 2) + 1
	maxFailure = replicationFactor - minSuccess

	// Skip those that have not heartbeated in a while. NB these are still
	// included in the calculation of minSuccess, so if too many failed ingesters
	// will cause the whole write to fail.
	liveIngesters = make([]IngesterDesc, 0, len(ingesters))
	for _, ingester := range ingesters {
		if r.IsHealthy(&ingester, op) {
			liveIngesters = append(liveIngesters, ingester)
		} else {
			maxFailure--
		}
	}

	// This is just a shortcut - if there are not minSuccess available ingesters,
	// after filtering out dead ones, don't even bother trying.
	if maxFailure < 0 || len(liveIngesters) < minSuccess {
		err = fmt.Errorf("at least %d live ingesters required, could only find %d",
			minSuccess, len(liveIngesters))
		return
	}

	return
}

// IsHealthy checks whether an ingester appears to be alive and heartbeating
func (r *Ring) IsHealthy(ingester *IngesterDesc, op Operation) bool {
	if op == Write && ingester.State != ACTIVE {
		return false
	} else if op == Read && ingester.State == JOINING {
		return false
	}
	return time.Now().Sub(time.Unix(ingester.Timestamp, 0)) <= r.cfg.HeartbeatTimeout
}

// ReplicationFactor of the ring.
func (r *Ring) ReplicationFactor() int {
	return r.cfg.ReplicationFactor
}
