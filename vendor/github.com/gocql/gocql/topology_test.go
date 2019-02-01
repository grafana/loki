package gocql

import (
	"fmt"
	"sort"
	"testing"
)

func TestPlacementStrategy_SimpleStrategy(t *testing.T) {
	host0 := &HostInfo{hostId: "0"}
	host25 := &HostInfo{hostId: "25"}
	host50 := &HostInfo{hostId: "50"}
	host75 := &HostInfo{hostId: "75"}

	tokenRing := []hostToken{
		{intToken(0), host0},
		{intToken(25), host25},
		{intToken(50), host50},
		{intToken(75), host75},
	}

	hosts := []*HostInfo{host0, host25, host50, host75}

	strat := &simpleStrategy{rf: 2}
	tokenReplicas := strat.replicaMap(hosts, tokenRing)
	if len(tokenReplicas) != len(tokenRing) {
		t.Fatalf("expected replica map to have %d items but has %d", len(tokenRing), len(tokenReplicas))
	}

	for token, replicas := range tokenReplicas {
		if len(replicas) != strat.rf {
			t.Errorf("expected to have %d replicas got %d for token=%v", strat.rf, len(replicas), token)
		}
	}

	for i, token := range tokenRing {
		replicas, ok := tokenReplicas[token.token]
		if !ok {
			t.Errorf("token %v not in replica map", token)
		}

		for j, replica := range replicas {
			exp := tokenRing[(i+j)%len(tokenRing)].host
			if exp != replica {
				t.Errorf("expected host %v to be a replica of %v got %v", exp, token, replica)
			}
		}
	}
}

func TestPlacementStrategy_NetworkStrategy(t *testing.T) {
	var (
		hosts  []*HostInfo
		tokens []hostToken
	)

	const (
		totalDCs   = 3
		racksPerDC = 3
		hostsPerDC = 5
	)

	dcRing := make(map[string][]hostToken, totalDCs)
	for i := 0; i < totalDCs; i++ {
		var dcTokens []hostToken
		dc := fmt.Sprintf("dc%d", i+1)

		for j := 0; j < hostsPerDC; j++ {
			rack := fmt.Sprintf("rack%d", (j%racksPerDC)+1)

			h := &HostInfo{hostId: fmt.Sprintf("%s:%s:%d", dc, rack, j), dataCenter: dc, rack: rack}

			token := hostToken{
				token: orderedToken([]byte(h.hostId)),
				host:  h,
			}

			tokens = append(tokens, token)
			dcTokens = append(dcTokens, token)

			hosts = append(hosts, h)
		}

		sort.Sort(&tokenRing{tokens: dcTokens})
		dcRing[dc] = dcTokens
	}

	if len(tokens) != hostsPerDC*totalDCs {
		t.Fatalf("expected %d tokens in the ring got %d", hostsPerDC*totalDCs, len(tokens))
	}
	sort.Sort(&tokenRing{tokens: tokens})

	strat := &networkTopology{
		dcs: map[string]int{
			"dc1": 1,
			"dc2": 2,
			"dc3": 3,
		},
	}

	var expReplicas int
	for _, rf := range strat.dcs {
		expReplicas += rf
	}

	tokenReplicas := strat.replicaMap(hosts, tokens)
	if len(tokenReplicas) != len(tokens) {
		t.Fatalf("expected replica map to have %d items but has %d", len(tokens), len(tokenReplicas))
	}

	for token, replicas := range tokenReplicas {
		if len(replicas) != expReplicas {
			t.Fatalf("expected to have %d replicas got %d for token=%v", expReplicas, len(replicas), token)
		}
	}

	for dc, rf := range strat.dcs {
		dcTokens := dcRing[dc]
		for i, th := range dcTokens {
			token := th.token
			allReplicas, ok := tokenReplicas[token]
			if !ok {
				t.Fatalf("token %v not in replica map", token)
			}

			var replicas []*HostInfo
			for _, replica := range allReplicas {
				if replica.dataCenter == dc {
					replicas = append(replicas, replica)
				}
			}

			if len(replicas) != rf {
				t.Fatalf("expected %d replicas in dc %q got %d", rf, dc, len(replicas))
			}

			var lastRack string
			for j, replica := range replicas {
				// expected is in the next rack
				var exp *HostInfo
				if lastRack == "" {
					// primary, first replica
					exp = dcTokens[(i+j)%len(dcTokens)].host
				} else {
					for k := 0; k < len(dcTokens); k++ {
						// walk around the ring from i + j to find the next host the
						// next rack
						p := (i + j + k) % len(dcTokens)
						h := dcTokens[p].host
						if h.rack != lastRack {
							exp = h
							break
						}
					}
					if exp.rack == lastRack {
						panic("no more racks")
					}
				}
				lastRack = replica.rack
			}
		}
	}
}
