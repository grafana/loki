package frontend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestRingStreamUsageGatherer_GetStreamUsage(t *testing.T) {
	tests := []struct {
		name                  string
		getStreamUsageRequest GetStreamUsageRequest
		// Instances contains the complete set of instances that should be mocked.
		// For example, if a test case is expected to make RPC calls to one instance,
		// then just one InstanceDesc is required.
		instances     []ring.InstanceDesc
		numPartitions int
		// The size of the following slices must match len(instances), where each
		// value contains the expected request/response for the instance at the
		// same index in the instances slice. If a request/response is not expected,
		// the value can be set to nil.
		expectedAssignedPartitionsRequest []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses    []*logproto.GetAssignedPartitionsResponse
		expectedStreamUsageRequests       []*logproto.GetStreamUsageRequest
		getStreamUsageResponses           []*logproto.GetStreamUsageResponse
		expectedResponses                 []GetStreamUsageResponse
	}{{
		// When there are no streams, no RPCs should be sent.
		name: "no streams",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{},
		},
		instances:                         []ring.InstanceDesc{{Addr: "instance-0"}},
		numPartitions:                     1,
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{nil},
		getAssignedPartitionsResponses:    []*logproto.GetAssignedPartitionsResponse{nil},
		expectedStreamUsageRequests:       []*logproto.GetStreamUsageRequest{nil},
		getStreamUsageResponses:           []*logproto.GetStreamUsageResponse{nil},
	}, {
		// When there is one stream, and one instance, the stream usage for that
		// stream should be queried from that instance.
		name: "one stream",
		getStreamUsageRequest: GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{0x1}, // Hash 0x1 maps to partition 0.
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
		numPartitions:                     1,
		expectedAssignedPartitionsRequest: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequests: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
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
			StreamHashes: []uint64{0x1}, // Hash 1 maps to partition 1.
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
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
		expectedStreamUsageRequests: []*logproto.GetStreamUsageRequest{nil, {
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{nil, {
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
			StreamHashes: []uint64{0x1}, // Hash 0x1 maps to partition 1.
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
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
		expectedStreamUsageRequests: []*logproto.GetStreamUsageRequest{nil, {
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{nil, {
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
			// Set up the mock clients, one for each set of mock RPC responses.
			mockClients := make([]*mockIngestLimitsClient, len(test.instances))
			for i := 0; i < len(test.instances); i++ {
				// These test cases assume one request/response per instance.
				expectedNumAssignedPartitionsRequests := 0
				if test.expectedAssignedPartitionsRequest[i] != nil {
					expectedNumAssignedPartitionsRequests = 1
				}
				expectedNumStreamUsageRequests := 0
				if test.expectedStreamUsageRequests[i] != nil {
					expectedNumStreamUsageRequests = 1
				}
				mockClients[i] = &mockIngestLimitsClient{
					t:                                     t,
					expectedAssignedPartitionsRequest:     test.expectedAssignedPartitionsRequest[i],
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					expectedStreamUsageRequest:            test.expectedStreamUsageRequests[i],
					getStreamUsageResponse:                test.getStreamUsageResponses[i],
					expectedNumAssignedPartitionsRequests: expectedNumAssignedPartitionsRequests,
					expectedNumStreamUsageRequests:        expectedNumStreamUsageRequests,
				}
				t.Cleanup(mockClients[i].AssertExpectedNumRequests)
			}
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()
			g := NewRingStreamUsageGatherer(readRing, clientPool, test.numPartitions, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resps, err := g.GetStreamUsage(ctx, test.getStreamUsageRequest)
			require.NoError(t, err)
			require.Equal(t, test.expectedResponses, resps)
		})
	}
}

func TestRingStreamUsageGatherer_GetZoneAwarePartitionConsumers(t *testing.T) {
	tests := []struct {
		name                               string
		instances                          []ring.InstanceDesc
		expectedAssignedPartitionsRequests []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses     []*logproto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs  []error
		expected                           map[string]map[int32]string
	}{{
		name: "single zone",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil},
		expected:                          map[string]map[int32]string{"a": {0: "instance-a-0"}},
	}, {
		name: "two zones",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[string]map[int32]string{
			"a": {0: "instance-a-0"},
			"b": {0: "instance-b-0"},
		},
	}, {
		name: "two zones, subset of partitions in zone b",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[string]map[int32]string{
			"a": {0: "instance-a-0", 1: "instance-a-0"},
			"b": {0: "instance-b-0"},
		},
	}, {
		name: "two zones, instance in zone b returns an error",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
			},
		}, nil},
		getAssignedPartitionsResponseErrs: []error{nil, errors.New("an unexpected error occurred")},
		expected: map[string]map[int32]string{
			"a": {0: "instance-a-0", 1: "instance-a-0"},
			"b": {},
		},
	}, {
		name: "two zones, all instances return an error",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses:     []*logproto.GetAssignedPartitionsResponse{{}, {}},
		getAssignedPartitionsResponseErrs:  []error{nil, nil, nil},
		expected:                           map[string]map[int32]string{"a": {}, "b": {}},
	}, {
		name: "two zones, different number of instances per zone",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-a-1",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil, nil},
		expected: map[string]map[int32]string{
			"a": {0: "instance-a-0", 1: "instance-a-1"},
			"b": {0: "instance-b-0", 1: "instance-b-0"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			clients := make([]*mockIngestLimitsClient, len(test.instances))
			for i := range test.instances {
				// These test cases assume one request/response per instance.
				expectedNumAssignedPartitionsRequests := 0
				if test.expectedAssignedPartitionsRequests[i] != nil {
					expectedNumAssignedPartitionsRequests = 1
				}
				clients[i] = &mockIngestLimitsClient{
					t:                                     t,
					expectedAssignedPartitionsRequest:     test.expectedAssignedPartitionsRequests[i],
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:      test.getAssignedPartitionsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: expectedNumAssignedPartitionsRequests,
				}
				t.Cleanup(clients[i].AssertExpectedNumRequests)
			}
			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, test.instances)
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()
			g := NewRingStreamUsageGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result, err := g.getZoneAwarePartitionConsumers(ctx, test.instances)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRingStreamUsageGatherer_GetPartitionConsumers(t *testing.T) {
	tests := []struct {
		name string
		// Instances contains the complete set of instances that should be mocked.
		// For example, if a test case is expected to make RPC calls to one instance,
		// then just one InstanceDesc is required.
		instances []ring.InstanceDesc
		// The size of the following slices must match len(instances), where each
		// value contains the expected request/response for the instance at the
		// same index in the instances slice. If a request/response is not expected,
		// the value can be set to nil.
		expectedAssignedPartitionsRequests []*logproto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses     []*logproto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs  []error
		// The expected result.
		expected map[int32]string
	}{{
		name: "single instance returns its partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil},
		expected: map[int32]string{
			0: "instance-0",
		},
	}, {
		name: "two instances return their separate partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[int32]string{
			0: "instance-0",
			1: "instance-1",
		},
	}, {
		name: "two instances claim the same partition, latest timestamp wins",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().Add(-time.Second).UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil, nil},
		expected: map[int32]string{
			0: "instance-1",
		},
	}, {
		// Even when one instance returns an error it should still return the
		// partitions for all remaining instances.
		name: "two instances, one returns error",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().Add(-time.Second).UnixNano(),
			},
		}, nil},
		getAssignedPartitionsResponseErrs: []error{
			nil,
			errors.New("an unexpected error occurred"),
		},
		expected: map[int32]string{
			0: "instance-0",
		},
	}, {
		// Even when all instances return an error, it should not return an
		// error.
		name: "all instances return error",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		expectedAssignedPartitionsRequests: []*logproto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses:     []*logproto.GetAssignedPartitionsResponse{nil, nil},
		getAssignedPartitionsResponseErrs: []error{
			errors.New("an unexpected error occurred"),
			errors.New("an unexpected error occurred"),
		},
		expected: map[int32]string{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			mockClients := make([]*mockIngestLimitsClient, len(test.instances))
			for i := range test.instances {
				// These test cases assume one request/response per instance.
				expectedNumAssignedPartitionsRequests := 0
				if test.expectedAssignedPartitionsRequests[i] != nil {
					expectedNumAssignedPartitionsRequests = 1
				}
				mockClients[i] = &mockIngestLimitsClient{
					t:                                     t,
					expectedAssignedPartitionsRequest:     test.expectedAssignedPartitionsRequests[i],
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:      test.getAssignedPartitionsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: expectedNumAssignedPartitionsRequests,
				}
				t.Cleanup(mockClients[i].AssertExpectedNumRequests)
			}
			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()
			g := NewRingStreamUsageGatherer(readRing, clientPool, 1, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result, err := g.getPartitionConsumers(ctx, test.instances)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRingStreamUsageGatherer_GetPartitionConsumers_IsCached(t *testing.T) {
	// Set up the mock clients, one for each pair of mock RPC responses.
	client0 := mockIngestLimitsClient{
		t: t,
		getAssignedPartitionsResponse: &logproto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		},
		expectedNumAssignedPartitionsRequests: 2,
	}
	t.Cleanup(client0.AssertExpectedNumRequests)
	client1 := mockIngestLimitsClient{
		t: t,
		getAssignedPartitionsResponse: &logproto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		},
		expectedNumAssignedPartitionsRequests: 2,
	}
	t.Cleanup(client1.AssertExpectedNumRequests)
	mockClients := []*mockIngestLimitsClient{&client0, &client1}
	instances := []ring.InstanceDesc{{Addr: "instance-0"}, {Addr: "instance-1"}}

	// Set up the mocked ring and client pool for the tests.
	readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, instances)

	// Set the cache TTL large enough that entries cannot expire (flake)
	// during slow test runs.
	cache := NewTTLCache[string, *logproto.GetAssignedPartitionsResponse](time.Minute)
	g := NewRingStreamUsageGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

	// Set a maximum upper bound on the test execution time.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.Equal(t, 0, client0.numAssignedPartitionsRequests)
	require.Equal(t, 0, client1.numAssignedPartitionsRequests)

	expected := map[int32]string{
		0: "instance-0",
		1: "instance-1",
	}

	// The first call should be a cache miss.
	actual, err := g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client0.numAssignedPartitionsRequests)
	require.Equal(t, 1, client1.numAssignedPartitionsRequests)

	// The second call should be a cache hit.
	actual, err = g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client0.numAssignedPartitionsRequests)
	require.Equal(t, 1, client1.numAssignedPartitionsRequests)

	// Expire the cache, it should be a cache miss.
	cache.Reset()

	// The third call should be a cache miss.
	actual, err = g.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 2, client0.numAssignedPartitionsRequests)
	require.Equal(t, 2, client1.numAssignedPartitionsRequests)
}
