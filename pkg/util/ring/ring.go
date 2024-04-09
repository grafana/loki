package ring

import (
	"hash/fnv"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/util"
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
	return util.StringsContain(rs.GetAddresses(), address), nil
}
