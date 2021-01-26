package ring

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
)

type ReplicationStrategy interface {
	// Filter out unhealthy instances and checks if there're enough instances
	// for an operation to succeed. Returns an error if there are not enough
	// instances.
	Filter(instances []IngesterDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration, zoneAwarenessEnabled bool) (healthy []IngesterDesc, maxFailures int, err error)
}

type defaultReplicationStrategy struct{}

func NewDefaultReplicationStrategy() ReplicationStrategy {
	return &defaultReplicationStrategy{}
}

// Filter decides, given the set of ingesters eligible for a key,
// which ingesters you will try and write to and how many failures you will
// tolerate.
// - Filters out dead ingesters so the one doesn't even try to write to them.
// - Checks there is enough ingesters for an operation to succeed.
// The ingesters argument may be overwritten.
func (s *defaultReplicationStrategy) Filter(ingesters []IngesterDesc, op Operation, replicationFactor int, heartbeatTimeout time.Duration, zoneAwarenessEnabled bool) ([]IngesterDesc, int, error) {
	// We need a response from a quorum of ingesters, which is n/2 + 1.  In the
	// case of a node joining/leaving, the actual replica set might be bigger
	// than the replication factor, so use the bigger or the two.
	if len(ingesters) > replicationFactor {
		replicationFactor = len(ingesters)
	}

	minSuccess := (replicationFactor / 2) + 1
	now := time.Now()

	// Skip those that have not heartbeated in a while. NB these are still
	// included in the calculation of minSuccess, so if too many failed ingesters
	// will cause the whole write to fail.
	for i := 0; i < len(ingesters); {
		if ingesters[i].IsHealthy(op, heartbeatTimeout, now) {
			i++
		} else {
			ingesters = append(ingesters[:i], ingesters[i+1:]...)
		}
	}

	// This is just a shortcut - if there are not minSuccess available ingesters,
	// after filtering out dead ones, don't even bother trying.
	if len(ingesters) < minSuccess {
		var err error

		if zoneAwarenessEnabled {
			err = fmt.Errorf("at least %d live replicas required across different availability zones, could only find %d", minSuccess, len(ingesters))
		} else {
			err = fmt.Errorf("at least %d live replicas required, could only find %d", minSuccess, len(ingesters))
		}

		return nil, 0, err
	}

	return ingesters, len(ingesters) - minSuccess, nil
}

type ignoreUnhealthyInstancesReplicationStrategy struct{}

func NewIgnoreUnhealthyInstancesReplicationStrategy() ReplicationStrategy {
	return &ignoreUnhealthyInstancesReplicationStrategy{}
}

func (r *ignoreUnhealthyInstancesReplicationStrategy) Filter(instances []IngesterDesc, op Operation, _ int, heartbeatTimeout time.Duration, _ bool) (healthy []IngesterDesc, maxFailures int, err error) {
	now := time.Now()
	// Filter out unhealthy instances.
	for i := 0; i < len(instances); {
		if instances[i].IsHealthy(op, heartbeatTimeout, now) {
			i++
		} else {
			instances = append(instances[:i], instances[i+1:]...)
		}
	}

	// We need at least 1 healthy instance no matter what is the replication factor set to.
	if len(instances) == 0 {
		return nil, 0, errors.New("at least 1 healthy replica required, could only find 0")
	}

	return instances, len(instances) - 1, nil
}

func (r *Ring) IsHealthy(ingester *IngesterDesc, op Operation, now time.Time) bool {
	return ingester.IsHealthy(op, r.cfg.HeartbeatTimeout, now)
}

// ReplicationFactor of the ring.
func (r *Ring) ReplicationFactor() int {
	return r.cfg.ReplicationFactor
}

// IngesterCount is number of ingesters in the ring
func (r *Ring) IngesterCount() int {
	r.mtx.RLock()
	c := len(r.ringDesc.Ingesters)
	r.mtx.RUnlock()
	return c
}
