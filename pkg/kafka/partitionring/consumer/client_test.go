package consumer

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

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
	var processingWg sync.WaitGroup

	// Create two consumers using our Client wrapper
	createConsumer := func(id string) *Client {
		cfg := kafka.Config{
			Address: addrs[0],
			Topic:   "test-topic",
		}

		// Track partition assignments for this consumer
		var assignedPartitions sync.Map
		var partitionsLock sync.Mutex

		client, err := NewGroupClient(cfg, mockReader, "test-group", kprom.NewMetrics("foo"), log.NewNopLogger(),
			kgo.ClientID(id),
			kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
				partitionsLock.Lock()
				defer partitionsLock.Unlock()
				t.Logf("%s assigned partitions: %v", id, assigned["test-topic"])
				for _, p := range assigned["test-topic"] {
					assignedPartitions.Store(p, struct{}{})
				}
			}),
			kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
				partitionsLock.Lock()
				defer partitionsLock.Unlock()
				t.Logf("%s revoked partitions: %v", id, revoked["test-topic"])
				for _, p := range revoked["test-topic"] {
					assignedPartitions.Delete(p)
				}
				// Wait for in-flight processing before revoking
				t.Logf("%s waiting for in-flight processing before revoke...", id)
				processingWg.Wait()
				t.Logf("%s completed revoke", id)
			}),
		)
		require.NoError(t, err)

		// Start consuming in a goroutine
		go func() {
			for {
				ctx := context.Background()
				records := client.PollFetches(ctx)
				if records == nil {
					return // client closed
				}

				if len(records.Records()) > 0 {
					processingWg.Add(1)
					go func(fetches kgo.Fetches) {
						defer processingWg.Done()

						// Verify we only got records for partitions we own
						for _, record := range fetches.Records() {
							_, ok := assignedPartitions.Load(record.Partition)
							require.True(t, ok, "%s received record for unassigned partition %d", id, record.Partition)

							// Track this record
							key := recordKey{record.Partition, record.Offset}
							if prev, loaded := processedRecords.LoadOrStore(key, id); loaded {
								t.Errorf("Record at partition %d offset %d processed twice! First by %v, then by %v",
									key.partition, key.offset, prev, id)
							}
						}

						// Commit the records
						if err := client.CommitRecords(context.Background(), fetches.Records()...); err != nil {
							t.Logf("%s error committing: %v", id, err)
						}
					}(records)
				}
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
	mockRing.partitionIDs = []int32{0, 1, 2}

	// Wait for rebalancing to occur and stabilize
	time.Sleep(7 * time.Second)

	// Change active partitions again
	t.Log("Changing active partitions from [0,1,2] to [0,1,2,3]")
	mockRing.partitionIDs = []int32{0, 1, 2, 3}

	// Wait for final rebalancing
	time.Sleep(7 * time.Second)

	// Stop producing
	cancel()

	// Wait for any in-flight processing to complete
	processingWg.Wait()

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
			Address: addrs[0],
			Topic:   "test-topic",
		}

		client, err := NewGroupClient(cfg, mockReader, "test-group", kprom.NewMetrics("foo"), log.NewNopLogger(),
			kgo.ClientID(id),
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
				if records == nil {
					return // client closed
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

	// Let initial setup stabilize and verify consumer1 is reading
	time.Sleep(2 * time.Second)
	require.Greater(t, lastOffset, int64(0), "consumer1 should have read some records from partition 0")
	initialOffset := lastOffset

	// Add second consumer and change active partitions
	t.Log("Adding consumer2 and changing active partitions from [0,1] to [0,1,2]")
	consumer2 := createConsumer("consumer2")
	defer consumer2.Close()
	mockRing.partitionIDs = []int32{0, 1, 2}

	// Let it run for a while
	time.Sleep(5 * time.Second)

	// Verify partition 0 continued reading without reset
	require.Greater(t, lastOffset, initialOffset,
		"partition 0 should have continued reading from last offset (%d)", initialOffset)
	t.Logf("Partition 0 read from offset %d to %d during rebalance", initialOffset, lastOffset)

	// Stop everything
	cancel()
}
