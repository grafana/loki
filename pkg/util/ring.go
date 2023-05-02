package util

import (
	"hash/fnv"
	"math"

	"github.com/grafana/dskit/ring"
)

// TokenFor generates a token used for finding ingesters from ring
func TokenFor(userID, labels string) uint32 {
	h := fnv.New32()
	_, _ = h.Write([]byte(userID))
	_, _ = h.Write([]byte(labels))
	return h.Sum32()
}

// IsInReplicationSet will query the provided ring for the provided key
// and see if the provided address is in the resulting ReplicationSet
func IsInReplicationSet(r ring.ReadRing, ringKey uint32, address string) (bool, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	rs, err := r.Get(ringKey, ring.Write, bufDescs, bufHosts, bufZones)
	if err != nil {
		return false, err
	}
	return StringsContain(rs.GetAddresses(), address), nil
}

// IsInReplicationSetWithFactor will query the provided ring for the provided key
// and see if the provided address is in the resulting ReplicationSet.
// Same as IsInReplicationSet, but you can additionally provide a replication factor.
func IsInReplicationSetWithFactor(r ring.DynamicReplicationReadRing, ringKey uint32, address string, rf int) (bool, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	rs, err := r.GetWithRF(ringKey, ring.Write, bufDescs, bufHosts, bufZones, rf)
	if err != nil {
		return false, err
	}
	return StringsContain(rs.GetAddresses(), address), nil
}

// DynamicReplicatioFactor returns a RF between ring.ReplicationFactor and ring.InstanceCount
// where a factor f=0 is mapped to the lower bound and f=1 is mapped to the upper bound.
func DynamicReplicationFactor(r ring.ReadRing, f float64) int {
	rf := r.ReplicationFactor()
	if f > 0 {
		f = math.Min(f, 1)
		rf = rf + int(float64(r.InstancesCount()-rf)*f)
	}
	return rf
}
