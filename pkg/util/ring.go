package util

import (
	"hash/fnv"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"

	util_log "github.com/grafana/loki/pkg/util/log"
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

	addrs := rs.GetAddresses()
	for _, a := range addrs {
		if a == address {
			return true, nil
		}
	}
	return false, nil
}

// IsAssignedKey replies wether the given instance address is in the ReplicationSet responsible for the given key or not, based on the tokens.
//
// The result will be defined based on the tokens assigned to each ring component, queried through the ring client.
func IsAssignedKey(ringClient ring.ReadRing, instanceAddress string, key string) bool {
	token := TokenFor(key, "" /* labels */)
	inSet, err := IsInReplicationSet(ringClient, token, instanceAddress)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error checking if key is in replicationset", "error", err, "key", key)
		return false
	}
	return inSet
}
