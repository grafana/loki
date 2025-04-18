package frontend

import (
	"context"
	"errors"
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
			for i := range test.expectedAssignedPartitionsRequest {
				clients[i] = &mockIngestLimitsClient{
					t:                                 t,
					expectedAssignedPartitionsRequest: test.expectedAssignedPartitionsRequest[i],
					expectedStreamUsageRequest:        test.expectedStreamUsageRequest[i],
					getAssignedPartitionsResponse:     test.getAssignedPartitionsResponses[i],
					getStreamUsageResponse:            test.getStreamUsageResponses[i],
				}
			}

			// Set up the instances for the ring.
			for i := range len(clients) {
				instances[i] = ring.InstanceDesc{
					Addr: fmt.Sprintf("instance-%d", i),
				}
			}

			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, instances)
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()

			g := NewRingStreamUsageGatherer(readRing, clientPool, numPartitions, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resps, err := g.GetStreamUsage(ctx, test.getStreamUsageRequest)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponses, resps)
		})
	}
}

func TestRingStreamUsageGatherer_GetPartitionConsumers(t *testing.T) {
	tests := []struct {
		name                              string
		instances                         []ring.InstanceDesc
		expectedAssignedPartitionsRequest []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses    []*logproto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs []error
		expected                          map[int32]string
	}{{
		name: "single instance returns its partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-1",
		}},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil},
		expected: map[int32]string{
			1: "instance-1",
		},
	}, {
		name: "two instances return their separate partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-1",
		}, {
			Addr: "instance-2",
		}},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				2: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[int32]string{
			1: "instance-1",
			2: "instance-2",
		},
	}, {
		name: "two instances claim the same partition, latest timestamp wins",
		instances: []ring.InstanceDesc{{
			Addr: "instance-1",
		}, {
			Addr: "instance-2",
		}},
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
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[int32]string{
			1: "instance-2",
		},
	}, {
		// This test asserts that even when one instance returns an error,
		// we can still get the assigned partitions for all remaining instances.
		name: "two instances, one returns error",
		instances: []ring.InstanceDesc{{
			Addr: "instance-1",
		}, {
			Addr: "instance-2",
		}},
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				1: time.Now().Add(-time.Second).UnixNano(),
			},
		}, {nil}},
		getAssignedPartitionsResponseErrs: []error{
			nil,
			errors.New("an unexpected error occurred"),
		},
		expected: map[int32]string{
			1: "instance-1",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			clients := make([]logproto.IngestLimitsClient, len(test.expectedAssignedPartitionsRequest))
			for i := range test.expectedAssignedPartitionsRequest {
				clients[i] = &mockIngestLimitsClient{
					t:                                 t,
					expectedAssignedPartitionsRequest: test.expectedAssignedPartitionsRequest[i],
					getAssignedPartitionsResponse:     test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:  test.getAssignedPartitionsResponseErrs[i],
				}
			}
			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, test.instances)
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()

			g := NewRingStreamUsageGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result, err := g.getPartitionConsumers(ctx, test.instances)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRingStreamUsageGatherer_GetPartitionConsumers_CacheHitsAndMisses(t *testing.T) {
	// Set up the mock clients, one for each pair of mock RPC responses.
	client1 := mockIngestLimitsClient{
		t:                                 t,
		expectedAssignedPartitionsRequest: &logproto.GetAssignedPartitionsRequest{},
		getAssignedPartitionsResponse: &logproto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		},
	}
	client2 := mockIngestLimitsClient{
		t:                                 t,
		expectedAssignedPartitionsRequest: &logproto.GetAssignedPartitionsRequest{},
		getAssignedPartitionsResponse: &logproto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				2: time.Now().UnixNano(),
			},
		},
	}
	clients := []logproto.IngestLimitsClient{&client1, &client2}
	instances := []ring.InstanceDesc{{Addr: "instance-1"}, {Addr: "instance-2"}}

	// Set up the mocked ring and client pool for the tests.
	readRing, clientPool := newMockRingWithClientPool(t, "test", clients, instances)

	// Set the cache TTL large enough that entries cannot expire (flake)
	// during slow test runs.
	cache := NewTTLCache[string, *logproto.GetAssignedPartitionsResponse](time.Minute)
	g := NewRingStreamUsageGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

	// Set a maximum upper bound on the test execution time.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.Equal(t, 0, client1.assignedPartitionsCallCount)
	require.Equal(t, 0, client2.assignedPartitionsCallCount)

	expected := map[int32]string{
		1: "instance-1",
		2: "instance-2",
	}

	// The first call should be a cache miss.
	actual, err := g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client1.assignedPartitionsCallCount)
	require.Equal(t, 1, client2.assignedPartitionsCallCount)

	// The second call should be a cache hit.
	actual, err = g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client1.assignedPartitionsCallCount)
	require.Equal(t, 1, client2.assignedPartitionsCallCount)

	// Expire the cache, it should be a cache miss.
	cache.Reset()

	// The third call should be a cache miss.
	actual, err = g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 2, client1.assignedPartitionsCallCount)
	require.Equal(t, 2, client2.assignedPartitionsCallCount)
}
