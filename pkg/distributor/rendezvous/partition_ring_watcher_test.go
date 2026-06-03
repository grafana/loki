package rendezvous

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "test-partition-ring"

func newTestKVClient(t *testing.T) kv.Client {
	t.Helper()
	kvClient, closer := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { assert.NoError(t, closer.Close()) })
	return kvClient
}

func writePartitionRing(t *testing.T, kvClient kv.Client, desc ring.PartitionRingDesc) {
	t.Helper()
	err := kvClient.CAS(context.Background(), testKey, func(_ interface{}) (interface{}, bool, error) {
		return &desc, true, nil
	})
	require.NoError(t, err)
}

func activePartitionRing(ids ...int32) ring.PartitionRingDesc {
	desc := ring.PartitionRingDesc{
		Partitions: make(map[int32]ring.PartitionDesc, len(ids)),
		Owners:     map[string]ring.OwnerDesc{},
	}
	for _, id := range ids {
		desc.Partitions[id] = ring.PartitionDesc{
			Id:             id,
			Tokens:         []uint32{uint32(id)},
			State:          ring.PartitionActive,
			StateTimestamp: time.Now().Unix(),
		}
	}
	return desc
}

func startWatcher(t *testing.T, kvClient kv.Client) *PartitionRingWatcher {
	t.Helper()
	watcher := New(Config{Key: testKey, HeartbeatTimeout: time.Hour}, kvClient, log.NewNopLogger())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), watcher))
	t.Cleanup(func() { assert.NoError(t, services.StopAndAwaitTerminated(context.Background(), watcher)) })
	return watcher
}

// TestPartitionWatcher_ReadsInitialState verifies that on start-up the watcher
// populates the sharder from whatever is already in the KV store.
func TestPartitionWatcher_ReadsInitialState(t *testing.T) {
	kvClient := newTestKVClient(t)
	writePartitionRing(t, kvClient, activePartitionRing(1, 2, 3))

	watcher := startWatcher(t, kvClient)

	sharder := watcher.ShuffleSharder()
	require.NotNil(t, sharder)

	require.ElementsMatch(t, []int32{1, 2, 3}, sharder.partitions)
}

// TestPartitionWatcher_NilSharderWhenEmpty verifies that ShuffleSharder() returns nil
// when the KV store has no data at start-up.
func TestPartitionWatcher_NilSharderWhenEmpty(t *testing.T) {
	kvClient := newTestKVClient(t)
	watcher := startWatcher(t, kvClient)
	assert.Nil(t, watcher.ShuffleSharder())
}

// TestPartitionWatcher_PicksUpChanges verifies that the watcher updates the
// sharder when the KV store value changes after start-up.
func TestPartitionWatcher_PicksUpChanges(t *testing.T) {
	kvClient := newTestKVClient(t)
	writePartitionRing(t, kvClient, activePartitionRing(1))

	watcher := startWatcher(t, kvClient)

	initial := watcher.ShuffleSharder()
	require.NotNil(t, initial)
	require.ElementsMatch(t, []int32{1}, initial.partitions)

	// Update KV store to a ring with only partition 2.
	writePartitionRing(t, kvClient, activePartitionRing(2))

	require.Eventually(t, func() bool {
		return watcher.ShuffleSharder() != initial
	}, 5*time.Second, 10*time.Millisecond, "watcher did not pick up KV change")

	updated := watcher.ShuffleSharder()
	require.NotNil(t, updated)
	require.ElementsMatch(t, []int32{2}, updated.partitions)
	require.ElementsMatch(t, []int32{1}, initial.partitions) // old caller still reflects pre-update state
}

// TestPartitionWatcher_InactivePartitionsExcluded verifies that only active
// partitions are included in the sharder; pending and inactive ones are ignored.
func TestPartitionWatcher_InactivePartitionsExcluded(t *testing.T) {
	kvClient := newTestKVClient(t)

	desc := ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			1: {Id: 1, State: ring.PartitionActive, Tokens: []uint32{1}, StateTimestamp: time.Now().Unix()},
			2: {Id: 2, State: ring.PartitionInactive, Tokens: []uint32{2}, StateTimestamp: time.Now().Unix()},
			3: {Id: 3, State: ring.PartitionPending, Tokens: []uint32{3}, StateTimestamp: time.Now().Unix()},
		},
		Owners: map[string]ring.OwnerDesc{},
	}
	writePartitionRing(t, kvClient, desc)

	watcher := startWatcher(t, kvClient)

	sharder := watcher.ShuffleSharder()
	require.NotNil(t, sharder)
	require.ElementsMatch(t, []int32{1}, sharder.partitions)
}

// TestPartitionWatcher_StalePartitionsExcluded verifies that active partitions
// whose StateTimestamp is older than HeartbeatTimeout are excluded from the sharder.
func TestPartitionWatcher_StalePartitionsExcluded(t *testing.T) {
	kvClient := newTestKVClient(t)

	desc := ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			1: {Id: 1, State: ring.PartitionActive, Tokens: []uint32{1}, StateTimestamp: time.Now().Unix()},
			2: {Id: 2, State: ring.PartitionActive, Tokens: []uint32{2}, StateTimestamp: time.Now().Add(-2 * time.Hour).Unix()},
		},
		Owners: map[string]ring.OwnerDesc{},
	}
	writePartitionRing(t, kvClient, desc)

	watcher := startWatcher(t, kvClient)

	sharder := watcher.ShuffleSharder()
	require.NotNil(t, sharder)
	require.ElementsMatch(t, []int32{1}, sharder.partitions, "stale partition 2 should be excluded")
}
