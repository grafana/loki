package client

import (
	"errors"

	"github.com/grafana/dskit/ring"
)

func NewRingServiceDiscovery(r ring.ReadRing) PoolServiceDiscovery {
	return func() ([]string, error) {
		replicationSet, err := r.GetAllHealthy(ring.Reporting)
		if errors.Is(err, ring.ErrEmptyRing) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		var addrs []string
		for _, instance := range replicationSet.Instances {
			addrs = append(addrs, instance.Addr)
		}
		return addrs, nil
	}
}

// NewRingsServiceDiscovery returns a PoolServiceDiscovery listing the deduplicated addresses
// of all healthy instances across the given rings.
func NewRingsServiceDiscovery(rings []ring.ReadRing) PoolServiceDiscovery {
	discoveries := make([]PoolServiceDiscovery, len(rings))
	for i, r := range rings {
		discoveries[i] = NewRingServiceDiscovery(r)
	}

	return func() ([]string, error) {
		var addrs []string
		seen := map[string]struct{}{}

		for _, discovery := range discoveries {
			ringAddrs, err := discovery()
			if err != nil {
				return nil, err
			}

			for _, addr := range ringAddrs {
				if _, ok := seen[addr]; ok {
					continue
				}
				seen[addr] = struct{}{}
				addrs = append(addrs, addr)
			}
		}

		return addrs, nil
	}
}
