package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPartitionRingService spins up a PartitionRingServices backed by
// an in-memory Consul KV. This avoids the complexity of memberlist for
// unit-level coverage of WaitForPartitions: only the watcher path
// matters here. Returns the service plus the KV client so tests can
// write descriptors to drive the watcher.
func newTestPartitionRingService(t *testing.T) (*PartitionRingWatcher, func(context.Context, *ring.PartitionRingDesc)) {
	t.Helper()

	logger := log.NewNopLogger()
	store, closer := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), logger, nil)
	t.Cleanup(func() { _ = closer.Close() })

	const key = "test/partition-ring"
	watcher := ring.NewPartitionRingWatcher("test", key, store, logger, prometheus.NewPedanticRegistry())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), watcher))
	t.Cleanup(func() {
		_ = services.StopAndAwaitTerminated(context.Background(), watcher)
	})

	s := &PartitionRingWatcher{
		cfg: RingConfig{
			Key: key,
		},
		logger:  logger,
		reg:     prometheus.NewPedanticRegistry(),
		watcher: watcher,
	}
	write := func(ctx context.Context, desc *ring.PartitionRingDesc) {
		require.NoError(t, store.CAS(ctx, key, func(_ interface{}) (interface{}, bool, error) {
			return desc, true, nil
		}))
	}
	return s, write
}

// TestWaitForPartitions_TimesOutOnEmptyRing locks in the "fail loudly"
// behavior: when the KV key is empty or unwritten, builder.starting()
// must NOT proceed. waitForPartitions returns a deadline error so
// operators see the misconfiguration (wrong ring.key, wrong
// cluster_label, ring never provisioned) in the pod's startup logs
// instead of a silent zero-throughput consumer group.
func TestWaitForPartitions_TimesOutOnEmptyRing(t *testing.T) {
	t.Parallel()

	s, _ := newTestPartitionRingService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := waitForPartitions(ctx, s, 200*time.Millisecond, log.NewNopLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become populated")
}

// TestWaitForPartitions_ReturnsOnceRingIsPopulated covers the happy
// path: after a partition descriptor is written to the KV the watcher
// eventually observes it and waitForPartitions returns nil. This is
// what unblocks builder startup against a real Loki memberlist cluster.
func TestWaitForPartitions_ReturnsOnceRingIsPopulated(t *testing.T) {
	t.Parallel()

	s, write := newTestPartitionRingService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Populate the ring shortly after waitForPartitions begins polling.
	go func() {
		time.Sleep(100 * time.Millisecond)
		desc := ring.NewPartitionRingDesc()
		desc.AddPartition(1, ring.PartitionActive, time.Now())
		write(ctx, desc)
	}()

	require.NoError(t, waitForPartitions(ctx, s, 3*time.Second, log.NewNopLogger()))
	assert.Greater(t, s.PartitionRing().PartitionsCount(), 0)
}

func TestPartitionRingMetrics_OnPartitionRingChanged(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewPedanticRegistry()
	m := newPartitionRingMetrics(reg)
	now := time.Now()

	// No partitions → no series.
	empty := ring.NewPartitionRingDesc()
	m.OnPartitionRingChanged(empty, empty)
	require.Equal(t, 0, testutil.CollectAndCount(m.activePartitions),
		"empty ring must produce no series")

	// Partition 0 Active, partition 1 Pending → only partition 0 emitted.
	withActive := ring.NewPartitionRingDesc()
	withActive.AddPartition(0, ring.PartitionActive, now)
	withActive.AddPartition(1, ring.PartitionPending, now)
	m.OnPartitionRingChanged(empty, withActive)
	require.Equal(t, 1, testutil.CollectAndCount(m.activePartitions),
		"only Active partitions emit a series")
	require.Equal(t, float64(1),
		testutil.ToFloat64(m.activePartitions.WithLabelValues("0")))

	// Demote 0 to Inactive, promote 1 to Active → series swap atomically.
	promoted := ring.NewPartitionRingDesc()
	promoted.AddPartition(0, ring.PartitionInactive, now)
	promoted.AddPartition(1, ring.PartitionActive, now)
	m.OnPartitionRingChanged(withActive, promoted)
	require.Equal(t, 1, testutil.CollectAndCount(m.activePartitions),
		"demoted partition must lose its series")
	require.Equal(t, float64(1),
		testutil.ToFloat64(m.activePartitions.WithLabelValues("1")))

	// Drop partition 1 from the ring entirely (uncommon — dskit usually
	// transitions to Deleted instead — but we cover it for safety against
	// future ring-pruning changes).
	shrunk := ring.NewPartitionRingDesc()
	shrunk.AddPartition(0, ring.PartitionInactive, now)
	m.OnPartitionRingChanged(promoted, shrunk)
	require.Equal(t, 0, testutil.CollectAndCount(m.activePartitions),
		"removed partition must lose its series")
}

// TestWaitForPartitions_RespectsContextCancellation guarantees that a
// service shutdown initiated mid-wait unblocks waitForPartitions
// promptly with ctx.Err(). Without this, a misconfigured ring would
// hold up service shutdown for the full WaitRingPopulatedTimeout window.
func TestWaitForPartitions_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	s, _ := newTestPartitionRingService(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := waitForPartitions(ctx, s, 10*time.Second, log.NewNopLogger())
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
