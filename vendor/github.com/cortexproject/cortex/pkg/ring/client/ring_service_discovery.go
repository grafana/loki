package client

import (
	"github.com/cortexproject/cortex/pkg/ring"
)

func NewRingServiceDiscovery(r ring.ReadRing) PoolServiceDiscovery {
	return func() ([]string, error) {
		replicationSet, err := r.GetAll(ring.Read)
		if err != nil {
			return nil, err
		}

		var addrs []string
		for _, instance := range replicationSet.Ingesters {
			addrs = append(addrs, instance.Addr)
		}
		return addrs, nil
	}
}
