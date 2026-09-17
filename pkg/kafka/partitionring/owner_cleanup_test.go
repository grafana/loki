package partitionring

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
)

const (
	testPartitionRingKey = "dataobj-consumer-partitions-key"
	testInstanceRingKey  = "dataobj-consumer"
)

func testConfig(t *testing.T, args ...string) OwnerCleanupConfig {
	t.Helper()

	var cfg OwnerCleanupConfig
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)
	require.NoError(t, fs.Parse(args))
	// Nothing to wait for in tests.
	cfg.PropagationDelay = 0
	require.NoError(t, cfg.Validate())
	return cfg
}

// newRings seeds a partition ring whose partition 0 is owned by a renamed
// (live) builder and by three leftovers, plus an instance ring in which only
// the builder still heartbeats.
func newRings(t *testing.T) (partitionStore, instanceStore kv.Client) {
	t.Helper()

	partitionStore, partitionCloser := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { require.NoError(t, partitionCloser.Close()) })

	instanceStore, instanceCloser := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { require.NoError(t, instanceCloser.Close()) })

	now := time.Now()
	require.NoError(t, partitionStore.CAS(context.Background(), testPartitionRingKey, func(any) (any, bool, error) {
		desc := ring.NewPartitionRingDesc()
		desc.AddPartition(0, ring.PartitionActive, now)
		desc.AddPartition(1, ring.PartitionActive, now)
		desc.AddOrUpdateOwner("dataobj-builder-0", ring.OwnerActive, 0, now)
		desc.AddOrUpdateOwner("dataobj-consumer-0", ring.OwnerActive, 0, now)
		desc.AddOrUpdateOwner("dataobj-consumer-1", ring.OwnerActive, 1, now)
		// Matches the pattern but is still heartbeating: an environment that
		// has not been renamed yet.
		desc.AddOrUpdateOwner("dataobj-consumer-7", ring.OwnerActive, 7, now)
		return desc, true, nil
	}))

	require.NoError(t, instanceStore.CAS(context.Background(), testInstanceRingKey, func(any) (any, bool, error) {
		desc := ring.NewDesc()
		desc.Ingesters = map[string]ring.InstanceDesc{
			"dataobj-builder-0":  {State: ring.ACTIVE, Timestamp: now.Unix()},
			"dataobj-consumer-7": {State: ring.ACTIVE, Timestamp: now.Unix()},
			// Registered but its heartbeat stopped hours ago.
			"dataobj-consumer-1": {State: ring.ACTIVE, Timestamp: now.Add(-3 * time.Hour).Unix()},
		}
		return desc, true, nil
	}))

	return partitionStore, instanceStore
}

func newCleaner(t *testing.T, cfg OwnerCleanupConfig, partitionStore, instanceStore kv.Client) *OwnerCleaner {
	t.Helper()

	cleaner, err := NewOwnerCleaner(
		cfg,
		"dataobj-consumer-partitions",
		testPartitionRingKey, partitionStore,
		testInstanceRingKey, instanceStore,
		log.NewNopLogger(),
	)
	require.NoError(t, err)
	return cleaner
}

func ownerIDs(t *testing.T, store kv.Client) []string {
	t.Helper()

	in, err := store.Get(context.Background(), testPartitionRingKey)
	require.NoError(t, err)

	var ids []string
	for id, owner := range ring.GetOrCreatePartitionRingDesc(in).Owners {
		if owner.State != ring.OwnerDeleted {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestOwnerCleaner_RemovesDepartedConsumersOnly(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	cleaner := newCleaner(t, testConfig(t, "-partition-ring-owner-cleanup.dry-run=false"), partitionStore, instanceStore)

	require.NoError(t, cleaner.run(context.Background()))

	require.ElementsMatch(t, []string{
		"dataobj-builder-0",  // does not match the pattern
		"dataobj-consumer-7", // matches, but its instance is still alive
	}, ownerIDs(t, partitionStore))
}

func TestOwnerCleaner_DryRunChangesNothing(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	before := ownerIDs(t, partitionStore)

	// dry-run defaults to true.
	cleaner := newCleaner(t, testConfig(t), partitionStore, instanceStore)
	require.NoError(t, cleaner.run(context.Background()))

	require.ElementsMatch(t, before, ownerIDs(t, partitionStore))
}

func TestOwnerCleaner_IgnoreLiveRemovesHeartbeatingOwner(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	cleaner := newCleaner(t, testConfig(t,
		"-partition-ring-owner-cleanup.dry-run=false",
		"-partition-ring-owner-cleanup.ignore-live=true",
	), partitionStore, instanceStore)

	require.NoError(t, cleaner.run(context.Background()))

	require.Equal(t, []string{"dataobj-builder-0"}, ownerIDs(t, partitionStore))
}

// The live check is a heartbeat comparison, so a long-enough threshold must
// treat a stale instance as alive and keep its owner.
func TestOwnerCleaner_LiveThresholdIsHonoured(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	cleaner := newCleaner(t, testConfig(t,
		"-partition-ring-owner-cleanup.dry-run=false",
		"-partition-ring-owner-cleanup.live-threshold=24h",
	), partitionStore, instanceStore)

	require.NoError(t, cleaner.run(context.Background()))

	// dataobj-consumer-1 heartbeat is 3h old, now within the threshold, so only
	// the owner with no instance-ring entry at all is removed.
	require.ElementsMatch(t, []string{
		"dataobj-builder-0",
		"dataobj-consumer-1",
		"dataobj-consumer-7",
	}, ownerIDs(t, partitionStore))
}

func TestOwnerCleaner_NoMatchesIsNotAnError(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	cleaner := newCleaner(t, testConfig(t,
		"-partition-ring-owner-cleanup.dry-run=false",
		"-partition-ring-owner-cleanup.owner-id-pattern=^nothing-matches-this-[0-9]+$",
	), partitionStore, instanceStore)

	require.NoError(t, cleaner.run(context.Background()))
	require.Len(t, ownerIDs(t, partitionStore), 4)
}

// running must ask Loki to shut down, otherwise the one-off pod would hang
// around after doing its work.
func TestOwnerCleaner_RunningStopsTheProcess(t *testing.T) {
	partitionStore, instanceStore := newRings(t)
	cleaner := newCleaner(t, testConfig(t, "-partition-ring-owner-cleanup.dry-run=false"), partitionStore, instanceStore)

	require.ErrorIs(t, cleaner.running(context.Background()), modules.ErrStopProcess)
}

func TestOwnerCleanupConfig(t *testing.T) {
	t.Run("default pattern spares builders", func(t *testing.T) {
		cfg := testConfig(t)
		require.True(t, cfg.pattern.MatchString("dataobj-consumer-0"))
		require.True(t, cfg.pattern.MatchString("dataobj-consumer-147"))
		require.False(t, cfg.pattern.MatchString("dataobj-builder-0"))
		// Anchoring must reject a near-miss rather than match it loosely.
		require.False(t, cfg.pattern.MatchString("other-dataobj-consumer-0"))
		require.False(t, cfg.pattern.MatchString("dataobj-consumer-0-shadow"))
	})

	t.Run("dry run is the default", func(t *testing.T) {
		require.True(t, testConfig(t).DryRun)
	})

	t.Run("rejects an invalid pattern", func(t *testing.T) {
		cfg := OwnerCleanupConfig{OwnerIDPattern: "([", LiveThreshold: time.Minute}
		require.ErrorContains(t, cfg.Validate(), "invalid owner_id_pattern")
	})

	t.Run("rejects an empty pattern", func(t *testing.T) {
		cfg := OwnerCleanupConfig{LiveThreshold: time.Minute}
		require.ErrorContains(t, cfg.Validate(), "owner_id_pattern is required")
	})

	t.Run("rejects a non-positive live threshold", func(t *testing.T) {
		cfg := OwnerCleanupConfig{OwnerIDPattern: defaultOwnerIDPattern}
		require.ErrorContains(t, cfg.Validate(), "live_threshold must be positive")
	})
}
