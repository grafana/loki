package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/bloomutils"
	"github.com/grafana/loki/pkg/chunkenc"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	util_log "github.com/grafana/loki/pkg/util/log"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	util_ring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

func TestCompactor_ownsTenant(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limits     Limits
		compactors int

		expectedCompactorsOwningTenant int
	}{
		{
			name: "no sharding with one instance",
			limits: mockLimits{
				shardSize: 0,
			},
			compactors:                     1,
			expectedCompactorsOwningTenant: 1,
		},
		{
			name: "no sharding with multiple instances",
			limits: mockLimits{
				shardSize: 0,
			},
			compactors:                     10,
			expectedCompactorsOwningTenant: 10,
		},
		{
			name: "sharding with one instance",
			limits: mockLimits{
				shardSize: 5,
			},
			compactors:                     1,
			expectedCompactorsOwningTenant: 1,
		},
		{
			name: "sharding with multiple instances",
			limits: mockLimits{
				shardSize: 5,
			},
			compactors:                     10,
			expectedCompactorsOwningTenant: 5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ringManagers []*lokiring.RingManager
			var compactors []*Compactor
			for i := 0; i < tc.compactors; i++ {
				var cfg Config
				cfg.RegisterFlags(flag.NewFlagSet("ring", flag.PanicOnError))
				cfg.Ring.KVStore.Store = "inmemory"
				cfg.Ring.InstanceID = fmt.Sprintf("bloom-compactor-%d", i)
				cfg.Ring.InstanceAddr = fmt.Sprintf("localhost-%d", i)

				ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, cfg.Ring, 1, cfg.Ring.NumTokens, util_log.Logger, prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, ringManager.StartAsync(context.Background()))

				shuffleSharding := util_ring.NewTenantShuffleSharding(ringManager.Ring, ringManager.RingLifecycler, tc.limits.BloomCompactorShardSize)

				compactor := &Compactor{
					cfg:      cfg,
					sharding: shuffleSharding,
					limits:   tc.limits,
				}

				ringManagers = append(ringManagers, ringManager)
				compactors = append(compactors, compactor)
			}
			defer func() {
				// Stop all rings and wait for them to stop.
				for _, ringManager := range ringManagers {
					ringManager.StopAsync()
					require.Eventually(t, func() bool {
						return ringManager.State() == services.Terminated
					}, 1*time.Minute, 100*time.Millisecond)
				}
			}()

			// Wait for all rings to see each other.
			for _, ringManager := range ringManagers {
				require.Eventually(t, func() bool {
					running := ringManager.State() == services.Running
					discovered := ringManager.Ring.InstancesCount() == tc.compactors
					return running && discovered
				}, 1*time.Minute, 100*time.Millisecond)
			}

			var compactorOwnsTenant int
			var compactorOwnershipRange []v1.FingerprintBounds
			for _, compactor := range compactors {
				ownershipRange, ownsTenant, err := compactor.ownsTenant("tenant")
				require.NoError(t, err)
				if ownsTenant {
					compactorOwnsTenant++
					compactorOwnershipRange = append(compactorOwnershipRange, ownershipRange...)
				}
			}
			require.Equal(t, tc.expectedCompactorsOwningTenant, compactorOwnsTenant)

			coveredKeySpace := v1.NewBounds(math.MaxUint64, 0)
			for i, boundsA := range compactorOwnershipRange {
				for j, boundsB := range compactorOwnershipRange {
					if i == j {
						continue
					}
					// Assert that the fingerprint key-space is not overlapping
					require.False(t, boundsA.Overlaps(boundsB))
				}

				if boundsA.Min < coveredKeySpace.Min {
					coveredKeySpace.Min = boundsA.Min
				}
				if boundsA.Max > coveredKeySpace.Max {
					coveredKeySpace.Max = boundsA.Max
				}

			}
			// Assert that the fingerprint key-space is complete
			require.True(t, coveredKeySpace.Equal(v1.NewBounds(0, math.MaxUint64)))
		})
	}
}

type mockLimits struct {
	shardSize int
}

func (m mockLimits) AllByUserID() map[string]*validation.Limits {
	panic("implement me")
}

func (m mockLimits) DefaultLimits() *validation.Limits {
	panic("implement me")
}

func (m mockLimits) VolumeMaxSeries(_ string) int {
	panic("implement me")
}

func (m mockLimits) BloomCompactorShardSize(_ string) int {
	return m.shardSize
}

func (m mockLimits) BloomCompactorEnabled(_ string) bool {
	return true
}

func (m mockLimits) BloomNGramLength(_ string) int {
	panic("implement me")
}

func (m mockLimits) BloomNGramSkip(_ string) int {
	panic("implement me")
}

func (m mockLimits) BloomFalsePositiveRate(_ string) float64 {
	panic("implement me")
}

func (m mockLimits) BloomBlockEncoding(_ string) string {
	return chunkenc.EncNone.String()
}

func (m mockLimits) BloomCompactorMaxBlockSize(_ string) int {
	panic("implement me")
}

func TestTokenRangesForInstance(t *testing.T) {
	desc := func(id int, tokens ...uint32) ring.InstanceDesc {
		return ring.InstanceDesc{Id: fmt.Sprintf("%d", id), Tokens: tokens}
	}

	tests := map[string]struct {
		input []ring.InstanceDesc
		exp   map[string]ring.TokenRanges
		err   bool
	}{
		"no nodes": {
			input: []ring.InstanceDesc{},
			exp: map[string]ring.TokenRanges{
				"0": {0, math.MaxUint32}, // have to put one in here to trigger test
			},
			err: true,
		},
		"one node": {
			input: []ring.InstanceDesc{
				desc(0, 0, 100),
			},
			exp: map[string]ring.TokenRanges{
				"0": {0, math.MaxUint32},
			},
		},
		"two nodes": {
			input: []ring.InstanceDesc{
				desc(0, 25, 75),
				desc(1, 10, 50, 100),
			},
			exp: map[string]ring.TokenRanges{
				"0": {10, 24, 50, 74},
				"1": {0, 9, 25, 49, 75, math.MaxUint32},
			},
		},
		"consecutive tokens": {
			input: []ring.InstanceDesc{
				desc(0, 99),
				desc(1, 100),
			},
			exp: map[string]ring.TokenRanges{
				"0": {0, 98, 100, math.MaxUint32},
				"1": {99, 99},
			},
		},
		"extremes": {
			input: []ring.InstanceDesc{
				desc(0, 0),
				desc(1, math.MaxUint32),
			},
			exp: map[string]ring.TokenRanges{
				"0": {math.MaxUint32, math.MaxUint32},
				"1": {0, math.MaxUint32 - 1},
			},
		},
	}

	for desc, test := range tests {
		t.Run(desc, func(t *testing.T) {
			for id := range test.exp {
				ranges, err := bloomutils.TokenRangesForInstance(id, test.input)
				if test.err {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, test.exp[id], ranges)
			}
		})
	}
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}
