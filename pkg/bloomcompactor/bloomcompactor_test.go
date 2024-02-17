package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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
				var ringCfg lokiring.RingConfig
				ringCfg.RegisterFlagsWithPrefix("", "", flag.NewFlagSet("ring", flag.PanicOnError))
				ringCfg.KVStore.Store = "inmemory"
				ringCfg.InstanceID = fmt.Sprintf("bloom-compactor-%d", i)
				ringCfg.InstanceAddr = fmt.Sprintf("localhost-%d", i)

				ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, ringCfg, 1, 1, util_log.Logger, prometheus.NewRegistry())
				require.NoError(t, err)
				require.NoError(t, ringManager.StartAsync(context.Background()))

				shuffleSharding := util_ring.NewTenantShuffleSharding(ringManager.Ring, ringManager.RingLifecycler, tc.limits.BloomCompactorShardSize)

				compactor := &Compactor{
					cfg: Config{
						Ring: ringCfg,
					},
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
					compactorOwnershipRange = append(compactorOwnershipRange, ownershipRange)
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

				// Assert that the fingerprint key-space is evenly distributed across the compactors
				// We do some adjustments if the key-space is not evenly distributable, so we use a delta of 10
				// to account for that and check that the key-space is reasonably evenly distributed.
				fpPerTenant := math.MaxUint64 / uint64(tc.expectedCompactorsOwningTenant)
				boundsLen := uint64(boundsA.Max - boundsA.Min)
				require.InDelta(t, fpPerTenant, boundsLen, 10)
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

func (m mockLimits) BloomCompactorChunksBatchSize(_ string) int {
	panic("implement me")
}

func (m mockLimits) BloomCompactorMaxTableAge(_ string) time.Duration {
	panic("implement me")
}

func (m mockLimits) BloomCompactorEnabled(_ string) bool {
	panic("implement me")
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

func (m mockLimits) BloomCompactorMaxBlockSize(_ string) int {
	panic("implement me")
}
