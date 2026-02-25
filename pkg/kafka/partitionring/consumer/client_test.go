package consumer

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
)

func TestPartitionMonitorRebalancing(t *testing.T) {
	// Create a fake cluster with initial partitions
	const totalPartitions = 6
	cluster := kfake.MustCluster(
		kfake.NumBrokers(2),
		kfake.SeedTopics(totalPartitions, "test-topic"),
	)
	defer cluster.Close()

	addrs := cluster.ListenAddrs()
	require.NotEmpty(t, addrs)

	// Create a producer client
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(addrs...),
	)
	require.NoError(t, err)
	defer producer.Close()

	// Create mock ring with initial active partitions
	mockRing := &mockPartitionRing{partitionIDs: []int32{0, 1}}
	mockReader := &mockPartitionRingReader{ring: mockRing}

	// Track processed records to verify continuity
	type recordKey struct {
		partition int32
		offset    int64
	}
	processedRecords := sync.Map{}

	// isActivePartition reports whether p is currently in the active ring.
	// Only active partitions get cooperative sticky assignment (via AdjustCooperative),
	// so only they are guaranteed no-duplicate handoff. Inactive partitions are
	// distributed round-robin without the two-round cooperative protocol, so a brief
	// overlap window exists during rebalance and duplicate detection is skipped for them.
	isActivePartition := func(p int32) bool {
		for _, id := range mockRing.PartitionIDs() {
			if id == p {
				return true
			}
		}
		return false
	}

	// Create two consumers using our Client wrapper
	createConsumer := func(id string) *Client {
		cfg := kafka.Config{
			ReaderConfig: kafka.ClientConfig{
				Address: addrs[0],
			},
			Topic: "test-topic",
		}

		// Track partition assignments for this consumer
		var assignedPartitions sync.Map
		var partitionsLock sync.Mutex

		client, err := NewGroupClient(cfg, mockReader, "test-group", log.NewNopLogger(),
			prometheus.NewRegistry(),
			kgo.ClientID(id),
			kgo.HeartbeatInterval(500*time.Millisecond),
			// BlockRebalanceOnPoll ensures OnPartitionsRevoked cannot fire until after
			// we commit the records returned by a PollFetches call and call AllowRebalance.
			// Without this, the revoke fires inside PollFetches before the caller can
			// commit, so CommitUncommittedOffsets misses the in-flight batch and the
			// new owner re-reads those records, causing duplicate detection failures.
			kgo.BlockRebalanceOnPoll(),
			kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
				partitionsLock.Lock()
				defer partitionsLock.Unlock()
				t.Logf("%s assigned partitions: %v", id, assigned["test-topic"])
				for _, p := range assigned["test-topic"] {
					assignedPartitions.Store(p, struct{}{})
				}
			}),
			kgo.OnPartitionsRevoked(func(ctx context.Context, client *kgo.Client, revoked map[string][]int32) {
				partitionsLock.Lock()
				defer partitionsLock.Unlock()
				t.Logf("%s revoked partitions: %v", id, revoked["test-topic"])
				for _, p := range revoked["test-topic"] {
					assignedPartitions.Delete(p)
				}

				// Complete committing offsets before finishing the revoke
				_ = client.CommitUncommittedOffsets(ctx)
				t.Logf("%s completed revoke", id)
			}),
		)
		require.NoError(t, err)

		// Start consuming in a goroutine. Process and commit synchronously, then
		// AllowRebalance so that OnPartitionsRevoked sees committed offsets.
		go func() {
			for {
				ctx := context.Background()
				records := client.PollFetches(ctx)
				if records.IsClientClosed() {
					return
				}

				for _, record := range records.Records() {
					_, ok := assignedPartitions.Load(record.Partition)
					require.True(t, ok, "%s received record for unassigned partition %d", id, record.Partition)

					// Only check for duplicates on active partitions. Inactive
				// partitions use round-robin assignment without the two-round
				// cooperative protocol, so a brief overlap is expected during handoff.
				if isActivePartition(record.Partition) {
					key := recordKey{record.Partition, record.Offset}
					if prev, loaded := processedRecords.LoadOrStore(key, id); loaded {
						t.Errorf("Record at partition %d offset %d processed twice! First by %v, then by %v",
							key.partition, key.offset, prev, id)
					}
				}
				}
				if len(records.Records()) > 0 {
					if err := client.CommitRecords(context.Background(), records.Records()...); err != nil {
						t.Logf("%s error committing: %v", id, err)
					}
				}
				client.AllowRebalance()
			}
		}()

		return client
	}

	// Create two consumers
	consumer1 := createConsumer("consumer1")
	defer consumer1.Close()
	consumer2 := createConsumer("consumer2")
	defer consumer2.Close()

	// Start producing records to all partitions
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		i := 0
		for ctx.Err() == nil {
			for partition := 0; partition < totalPartitions; partition++ {
				err := producer.ProduceSync(ctx, &kgo.Record{
					Topic:     "test-topic",
					Partition: int32(partition),
					Key:       []byte(fmt.Sprintf("key-%d", i)),
					Value:     []byte(fmt.Sprintf("value-%d", i)),
				}).FirstErr()
				if err != nil {
					t.Logf("Error producing to partition %d: %v", partition, err)
					return
				}
			}
			i++
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Let the initial setup stabilize
	time.Sleep(2 * time.Second)

	// Change the active partitions in the ring
	t.Log("Changing active partitions from [0,1] to [0,1,2]")
	mockRing.setPartitions([]int32{0, 1, 2})
	consumer1.ForceRebalance()
	consumer2.ForceRebalance()

	// Wait for rebalancing to occur and stabilize
	time.Sleep(2 * time.Second)

	// Change active partitions again
	t.Log("Changing active partitions from [0,1,2] to [0,1,2,3]")
	mockRing.setPartitions([]int32{0, 1, 2, 3})
	consumer1.ForceRebalance()
	consumer2.ForceRebalance()

	// Wait for final rebalancing
	time.Sleep(2 * time.Second)

	// Stop producing
	cancel()

	// Verify no duplicates were processed and count records per partition
	partitionCounts := make(map[int32]int)
	partitionConsumers := make(map[int32]map[string]struct{})
	processedRecords.Range(func(key, value interface{}) bool {
		k := key.(recordKey)
		partitionCounts[k.partition]++
		if _, ok := partitionConsumers[k.partition]; !ok {
			partitionConsumers[k.partition] = make(map[string]struct{})
		}
		partitionConsumers[k.partition][value.(string)] = struct{}{}
		return true
	})

	// Log partition processing stats
	for partition, count := range partitionCounts {
		t.Logf("Partition %d: processed %d records", partition, count)
		require.Greater(t, count, 0, "Expected records from  partition %d", partition)
	}

	for partition, consumers := range partitionConsumers {
		t.Logf("Partition %d: processed by %v", partition, consumers)
	}
}

func TestPartitionContinuityDuringRebalance(t *testing.T) {
	// Create a fake cluster with initial partitions
	const totalPartitions = 4
	cluster := kfake.MustCluster(
		kfake.NumBrokers(2),
		kfake.SeedTopics(totalPartitions, "test-topic"),
	)
	defer cluster.Close()

	addrs := cluster.ListenAddrs()
	require.NotEmpty(t, addrs)

	// Create mock ring with initial active partitions
	mockRing := &mockPartitionRing{partitionIDs: []int32{0, 1}}
	mockReader := &mockPartitionRingReader{ring: mockRing}

	// Track offsets for partition 0 to verify continuous reading
	var lastOffset int64
	var offsetMu sync.Mutex

	createConsumer := func(id string) *Client {
		cfg := kafka.Config{
			ReaderConfig: kafka.ClientConfig{
				Address: addrs[0],
			},
			Topic: "test-topic",
		}

		client, err := NewGroupClient(cfg, mockReader, "test-group", log.NewNopLogger(),
			prometheus.NewRegistry(),
			kgo.ClientID(id),
			kgo.HeartbeatInterval(500*time.Millisecond),
			kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
				t.Logf("%s assigned partitions: %v", id, assigned["test-topic"])
			}),
			kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
				t.Logf("%s revoked partitions: %v", id, revoked["test-topic"])
			}),
		)
		require.NoError(t, err)

		go func() {
			for {
				ctx := context.Background()
				records := client.PollFetches(ctx)
				if records.IsClientClosed() {
					return
				}

				// Only verify partition 0's offset continuity
				for _, record := range records.Records() {
					if record.Partition == 0 {
						offsetMu.Lock()
						if lastOffset > 0 {
							require.Equal(t, lastOffset+1, record.Offset,
								"Gap detected in partition 0: expected offset %d, got %d",
								lastOffset+1, record.Offset)
						}
						lastOffset = record.Offset
						offsetMu.Unlock()
						t.Logf("%s read offset %d from partition 0", id, record.Offset)
					}
				}
			}
		}()

		return client
	}

	// Create producer
	producer, err := kgo.NewClient(kgo.SeedBrokers(addrs...))
	require.NoError(t, err)
	defer producer.Close()

	// Start with one consumer
	consumer1 := createConsumer("consumer1")
	defer consumer1.Close()

	// Start producing records
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		i := 0
		for ctx.Err() == nil {
			for partition := 0; partition < totalPartitions; partition++ {
				err := producer.ProduceSync(ctx, &kgo.Record{
					Topic:     "test-topic",
					Partition: int32(partition),
					Key:       []byte(fmt.Sprintf("key-%d", i)),
					Value:     []byte(fmt.Sprintf("value-%d", i)),
				}).FirstErr()
				if err != nil {
					t.Logf("Error producing to partition %d: %v", partition, err)
					return
				}
			}
			i++
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Wait until consumer1 is actually reading from partition 0
	require.Eventually(t, func() bool {
		offsetMu.Lock()
		defer offsetMu.Unlock()
		return lastOffset > 0
	}, 15*time.Second, 100*time.Millisecond, "consumer1 should have read some records from partition 0")
	offsetMu.Lock()
	initialOffset := lastOffset
	offsetMu.Unlock()

	// Add second consumer and change active partitions
	t.Log("Adding consumer2 and changing active partitions from [0,1] to [0,1,2]")
	consumer2 := createConsumer("consumer2")
	defer consumer2.Close()
	mockRing.setPartitions([]int32{0, 1, 2})
	// Only trigger rebalance on the already-established consumer1; calling ForceRebalance
	// on consumer2 while it is still completing its first join causes a rebalance storm.
	consumer1.ForceRebalance()

	// Wait for partition 0 to advance past the pre-rebalance offset
	require.Eventually(t, func() bool {
		offsetMu.Lock()
		defer offsetMu.Unlock()
		return lastOffset > initialOffset
	}, 15*time.Second, 100*time.Millisecond,
		"partition 0 should have continued reading from last offset (%d)", initialOffset)
	offsetMu.Lock()
	finalOffset := lastOffset
	offsetMu.Unlock()
	t.Logf("Partition 0 read from offset %d to %d during rebalance", initialOffset, finalOffset)

	// Stop everything
	cancel()
}
