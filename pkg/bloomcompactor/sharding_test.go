package bloomcompactor

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/pkg/util/log"
	lokiring "github.com/grafana/loki/pkg/util/ring"
	"github.com/grafana/loki/pkg/validation"
)

func TestShuffleSharding(t *testing.T) {
	const shardSize = 2
	const rings = 4
	const tenants = 2000
	const jobsPerTenant = 200

	var limits validation.Limits
	limits.RegisterFlags(flag.NewFlagSet("limits", flag.PanicOnError))
	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	var ringManagers []*lokiring.RingManager
	var shards []*ShuffleShardingStrategy
	for i := 0; i < rings; i++ {
		var ringCfg lokiring.RingConfig
		ringCfg.RegisterFlagsWithPrefix("", "", flag.NewFlagSet("ring", flag.PanicOnError))
		ringCfg.KVStore.Store = "inmemory"
		ringCfg.InstanceID = fmt.Sprintf("bloom-compactor-%d", i)
		ringCfg.InstanceAddr = fmt.Sprintf("localhost-%d", i)

		ringManager, err := lokiring.NewRingManager("bloom-compactor", lokiring.ServerMode, ringCfg, 1, 1, util_log.Logger, prometheus.NewRegistry())
		require.NoError(t, err)
		require.NoError(t, ringManager.StartAsync(context.Background()))

		sharding := NewShuffleShardingStrategy(ringManager.Ring, ringManager.RingLifecycler, mockLimits{
			Overrides:               overrides,
			bloomCompactorShardSize: shardSize,
		})

		ringManagers = append(ringManagers, ringManager)
		shards = append(shards, sharding)
	}

	// Wait for all rings to see each other.
	for i := 0; i < rings; i++ {
		require.Eventually(t, func() bool {
			running := ringManagers[i].State() == services.Running
			discovered := ringManagers[i].Ring.InstancesCount() == rings
			return running && discovered
		}, 1*time.Minute, 100*time.Millisecond)
	}

	// This is kind of an un-deterministic test, because sharding is random
	// and the seed is initialized by the ring lib.
	// Here we'll generate a bunch of tenants and test that if the sharding doesn't own the tenant,
	// that's because the tenant is owned by other ring instances.
	shard := shards[0]
	otherShards := shards[1:]
	var ownedTenants, ownedJobs int
	for i := 0; i < tenants; i++ {
		tenant := fmt.Sprintf("tenant-%d", i)
		ownsTenant := shard.OwnsTenant(tenant)

		var tenantOwnedByOther int
		for _, other := range otherShards {
			otherOwns := other.OwnsTenant(tenant)
			if otherOwns {
				tenantOwnedByOther++
			}
		}

		// If this shard owns the tenant, shardSize-1 other members should also own the tenant.
		// Otherwise, shardSize other members should own the tenant.
		if ownsTenant {
			require.Equal(t, shardSize-1, tenantOwnedByOther)
			ownedTenants++
		} else {
			require.Equal(t, shardSize, tenantOwnedByOther)
		}

		for j := 0; j < jobsPerTenant; j++ {
			lbls := labels.FromStrings("namespace", fmt.Sprintf("namespace-%d", j))
			fp := model.Fingerprint(lbls.Hash())
			ownsFingerprint, err := shard.OwnsFingerprint(tenant, uint64(fp))
			require.NoError(t, err)

			var jobOwnedByOther int
			for _, other := range otherShards {
				otherOwns, err := other.OwnsFingerprint(tenant, uint64(fp))
				require.NoError(t, err)
				if otherOwns {
					jobOwnedByOther++
				}
			}

			// If this shard owns the job, no one else should own the job.
			// And if this shard doesn't own the job, only one of the other shards should own the job.
			if ownsFingerprint {
				require.Equal(t, 0, jobOwnedByOther)
				ownedJobs++
			} else {
				require.Equal(t, 1, jobOwnedByOther)
			}
		}
	}

	t.Logf("owned tenants: %d (out of %d)", ownedTenants, tenants)
	t.Logf("owned jobs: %d (out of %d)", ownedJobs, tenants*jobsPerTenant)

	// Stop all rings and wait for them to stop.
	for i := 0; i < rings; i++ {
		ringManagers[i].StopAsync()
		require.Eventually(t, func() bool {
			return ringManagers[i].State() == services.Terminated
		}, 1*time.Minute, 100*time.Millisecond)
	}
}

type mockLimits struct {
	*validation.Overrides
	bloomCompactorShardSize int
}

func (m mockLimits) BloomCompactorShardSize(_ string) int {
	return m.bloomCompactorShardSize
}
