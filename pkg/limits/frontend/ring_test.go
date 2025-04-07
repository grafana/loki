package frontend

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestRingStreamUsageGatherer_GetStreamUsage(t *testing.T) {
	const numPartitions = 2 // Using 2 partitions for simplicity in tests
	tests := []struct {
		name                              string
		getStreamUsageRequest             GetStreamUsageRequest
		expectedAssignedPartitionsRequest []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses    []*logproto.GetAssignedPartitionsResponse
		expectedStreamUsageRequest        []*logproto.GetStreamUsageRequest
		getStreamUsageResponses           []*logproto.GetStreamUsageResponse
		expectedResponses                 []GetStreamUsageResponse
	}{{
		// When there are no streams, no RPCs should be sent.
		name: "no streams",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{},
		},
	}, {
		// When there is one stream, and one instance, the stream usage for that
		// stream should be queried from that instance.
		name: "one stream",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{1}, // Hash 1 maps to partition 1
		},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          10,
		}},
		expectedResponses: []GetStreamUsageResponse{{
			Addr: "instance-0",
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
	}, {
		// When there is one stream, and two instances each owning separate
		// partitions, only the instance owning the partition for the stream hash
		// should be queried.
		name: "one stream two instances",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{1}, // Hash 1 maps to partition 1
		},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{}, {
			Tenant:       "test",
			StreamHashes: []uint64{1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{}, {
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          10,
		}},
		expectedResponses: []GetStreamUsageResponse{{
			Addr: "instance-1",
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
	}, {
		// When there is one stream, and two instances owning overlapping
		// partitions, only the instance with the latest timestamp for the relevant
		// partition should be queried.
		name: "one stream two instances, overlapping partition ownership",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{1}, // Hash 1 maps to partition 1
		},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().Add(-time.Second).UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{}, {
			Tenant:       "test",
			StreamHashes: []uint64{1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{}, {
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          10,
		}},
		expectedResponses: []GetStreamUsageResponse{{
			Addr: "instance-1",
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			clients := make([]logproto.IngestLimitsClient, len(test.expectedAssignedPartitionsRequest))
			instances := make([]ring.InstanceDesc, len(clients))

			// Set up the mock clients for the assigned partitions requests.
			// Note: Every client will return a response for its assigned partitions.
			for i := range test.expectedAssignedPartitionsRequest {
				clients[i] = &mockIngestLimitsClient{
					t:                                 t,
					expectedAssignedPartitionsRequest: test.expectedAssignedPartitionsRequest[i],
					getAssignedPartitionsResponse:     test.getAssignedPartitionsResponses[i],
				}
			}

			// Set up the mock clients for the stream usage requests.
			// Note: Not every client will have a stream usage request because the
			// requested stream hashes are owned only by a subset of partition consumers.
			for i := range test.expectedStreamUsageRequest {
				clients[i].(*mockIngestLimitsClient).expectedStreamUsageRequest = test.expectedStreamUsageRequest[i]
				clients[i].(*mockIngestLimitsClient).getStreamUsageResponse = test.getStreamUsageResponses[i]
			}

			// Set up the instances for the ring.
			for i := range len(clients) {
				instances[i] = ring.InstanceDesc{
					Addr: fmt.Sprintf("instance-%d", i),
				}
			}

			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, instances)
			cache := NewPartitionConsumerCache(1 * time.Millisecond)

			g := NewRingStreamUsageGatherer(readRing, clientPool, log.NewNopLogger(), cache, numPartitions)

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resps, err := g.GetStreamUsage(ctx, test.getStreamUsageRequest)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponses, resps)
		})
	}
}

func TestRingStreamUsageGatherer_GetStreamUsage_Cache(t *testing.T) {
	const numPartitions = 2 // Using 2 partitions for simplicity in tests
	const cacheTTL = 1 * time.Second

	tests := []struct {
		name                                string
		getStreamUsageRequest               GetStreamUsageRequest
		expectedAssignedPartitionsRequest   []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses      []*logproto.GetAssignedPartitionsResponse
		expectedStreamUsageRequest          []*logproto.GetStreamUsageRequest
		getStreamUsageResponses             []*logproto.GetStreamUsageResponse
		expectedResponses                   []GetStreamUsageResponse
		expectedAssignedPartitionsCallCount int
		waitBetweenCalls                    time.Duration
	}{{
		name: "cache hit - second call within TTL should not make partition request",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{1}, // Hash 1 maps to partition 1
		},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          10,
		}},
		expectedResponses: []GetStreamUsageResponse{{
			Addr: "instance-0",
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
		expectedAssignedPartitionsCallCount: 1,
		waitBetweenCalls:                    500 * time.Millisecond, // Wait less than TTL
	}, {
		name: "cache miss - second call after TTL should make new partition request",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{1}, // Hash 1 maps to partition 1
		},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          10,
		}},
		expectedResponses: []GetStreamUsageResponse{{
			Addr: "instance-0",
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
		expectedAssignedPartitionsCallCount: 2,
		waitBetweenCalls:                    2 * time.Second, // Wait longer than TTL
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients for the first call
			clients := make([]logproto.IngestLimitsClient, len(test.expectedAssignedPartitionsRequest))
			instances := make([]ring.InstanceDesc, len(clients))

			// Set up the mock clients for the assigned partitions requests
			for i := range test.expectedAssignedPartitionsRequest {
				clients[i] = &mockIngestLimitsClient{
					t:                                 t,
					expectedAssignedPartitionsRequest: test.expectedAssignedPartitionsRequest[i],
					getAssignedPartitionsResponse:     test.getAssignedPartitionsResponses[i],
				}
			}

			// Set up the mock clients for the stream usage requests
			for i := range test.expectedStreamUsageRequest {
				clients[i].(*mockIngestLimitsClient).expectedStreamUsageRequest = test.expectedStreamUsageRequest[i]
				clients[i].(*mockIngestLimitsClient).getStreamUsageResponse = test.getStreamUsageResponses[i]
			}

			// Set up the instances for the ring
			for i := range len(clients) {
				instances[i] = ring.InstanceDesc{
					Addr: fmt.Sprintf("instance-%d", i),
				}
			}

			// Set up the mocked ring and client pool for the tests
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, instances)

			// Create cache with TTL
			cache := NewPartitionConsumerCache(cacheTTL)
			g := NewRingStreamUsageGatherer(readRing, clientPool, log.NewNopLogger(), cache, numPartitions)

			// First call
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resps, err := g.GetStreamUsage(ctx, test.getStreamUsageRequest)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponses, resps)

			// Wait between calls
			time.Sleep(test.waitBetweenCalls)

			if test.waitBetweenCalls > cacheTTL {
				for i := range len(clients) {
					clients[i].(*mockIngestLimitsClient).expectedAssignedPartitionsRequest = &logproto.GetAssignedPartitionsRequest{}
				}
			}

			// Second call
			resps, err = g.GetStreamUsage(ctx, test.getStreamUsageRequest)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponses, resps)

			for i := range len(clients) {
				require.Equal(t, test.expectedAssignedPartitionsCallCount, clients[i].(*mockIngestLimitsClient).assignedPartitionsCallCount)
			}
		})
	}
}
