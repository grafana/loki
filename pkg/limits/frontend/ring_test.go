package frontend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestRingGatherer_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name    string
		request *proto.ExceedsLimitsRequest
		// Instances contains the complete set of instances that should be mocked.
		// For example, if a test case is expected to make RPC calls to one instance,
		// then just one InstanceDesc is required.
		instances     []ring.InstanceDesc
		numPartitions int
		// The size of the following slices must match len(instances), where each
		// value contains the expected request/response for the instance at the
		// same index in the instances slice. If a request/response is not expected,
		// the value can be set to nil.
		expectedAssignedPartitionsRequest []*proto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses    []*proto.GetAssignedPartitionsResponse
		expectedExceedsLimitsRequests     []*proto.ExceedsLimitsRequest
		exceedsLimitsResponses            []*proto.ExceedsLimitsResponse
		exceedsLimitsResponseErrs         []error
		expected                          []*proto.ExceedsLimitsResponse
		expectedErr                       string
	}{{
		// When there are no streams, no RPCs should be sent.
		name: "no streams",
		request: &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: nil,
		},
		instances:                         []ring.InstanceDesc{{Addr: "instance-0"}},
		numPartitions:                     1,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{nil},
		getAssignedPartitionsResponses:    []*proto.GetAssignedPartitionsResponse{nil},
		expectedExceedsLimitsRequests:     []*proto.ExceedsLimitsRequest{nil},
		exceedsLimitsResponses:            []*proto.ExceedsLimitsResponse{nil},
		exceedsLimitsResponseErrs:         []error{nil},
	}, {
		// When there is one instance owning all partitions, that instance is
		// responsible for enforcing limits of all streams.
		name: "one stream one instance",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1, // 0x1 is assigned to partition 0.
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
		numPartitions:                     1,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil},
		expected: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
	}, {
		// When there are two instances, each instance is responsible for
		// enforcing limits on just the streams that shard to its consumed
		// partitions. But when we have one stream, just one instance
		// should be called to enforce limits.
		name: "one stream two instances",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1, // 0x1 is assigned to partition 1.
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{nil, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{nil, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
	}, {
		// When there are two streams and two instances, but all streams
		// shard to one partition, just the instance that consumes that
		// partition should be called to enforce limits.
		name: "two streams, two instances, all streams to one partition",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1, // 0x1 is assigned to partition 1.
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x3, // 0x3 is also assigned to partition 1.
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{nil, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x3,
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{nil, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
	}, {
		// When there are two streams and two instances, and each stream
		// shards to different partitions, all instances should be called
		// called to enforce limits.
		name: "two streams, two instances, one stream each",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1, // 0x1 is assigned to partition 1.
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x2, // 0x2 is also assigned to partition 0.
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x2,
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		}, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
	}, {
		// When one instance returns an error, the entire request is failed.
		name: "two streams, two instances, one instance returns error",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1, // 0x1 is assigned to partition 1.
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x2, // 0x2 is also assigned to partition 0.
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions:                     2,
		expectedAssignedPartitionsRequest: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x2,
				EntriesSize:            0x4,
				StructuredMetadataSize: 0x5,
			}},
		}, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}, nil},
		exceedsLimitsResponseErrs: []error{nil, errors.New("an unexpected error occurred")},
		expectedErr:               "an unexpected error occurred",
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
				expectedNumExceedsLimitsRequests := 0
				if test.expectedExceedsLimitsRequests[i] != nil {
					expectedNumExceedsLimitsRequests = 1
				}
				mockClients[i] = &mockIngestLimitsClient{
					t:                                     t,
					expectedAssignedPartitionsRequest:     test.expectedAssignedPartitionsRequest[i],
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					expectedExceedsLimitsRequest:          test.expectedExceedsLimitsRequests[i],
					exceedsLimitsResponse:                 test.exceedsLimitsResponses[i],
					exceedsLimitsResponseErr:              test.exceedsLimitsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: expectedNumAssignedPartitionsRequests,
					expectedNumExceedsLimitsRequests:      expectedNumExceedsLimitsRequests,
				}
				t.Cleanup(mockClients[i].AssertExpectedNumRequests)
			}
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := NewNopCache[string, *proto.GetAssignedPartitionsResponse]()
			g := NewRingGatherer(readRing, clientPool, test.numPartitions, cache, log.NewNopLogger())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			actual, err := g.ExceedsLimits(ctx, test.request)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				require.Nil(t, actual)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, test.expected, actual)
			}
		})
	}
}

func TestRingStreamUsageGatherer_GetZoneAwarePartitionConsumers(t *testing.T) {
	tests := []struct {
		name                               string
		instances                          []ring.InstanceDesc
		expectedAssignedPartitionsRequests []*proto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses     []*proto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs  []error
		expected                           map[string]map[int32]string
	}{{
		name: "single zone",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}},
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses:     []*proto.GetAssignedPartitionsResponse{{}, {}},
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
			cache := NewNopCache[string, *proto.GetAssignedPartitionsResponse]()
			g := NewRingGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

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
		expectedAssignedPartitionsRequests []*proto.GetAssignedPartitionsRequest
		getAssignedPartitionsResponses     []*proto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs  []error
		// The expected result.
		expected map[int32]string
	}{{
		name: "single instance returns its partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
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
		expectedAssignedPartitionsRequests: []*proto.GetAssignedPartitionsRequest{{}, {}},
		getAssignedPartitionsResponses:     []*proto.GetAssignedPartitionsResponse{nil, nil},
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
			cache := NewNopCache[string, *proto.GetAssignedPartitionsResponse]()
			g := NewRingGatherer(readRing, clientPool, 1, cache, log.NewNopLogger())

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
		getAssignedPartitionsResponse: &proto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		},
		expectedNumAssignedPartitionsRequests: 2,
	}
	t.Cleanup(client0.AssertExpectedNumRequests)
	client1 := mockIngestLimitsClient{
		t: t,
		getAssignedPartitionsResponse: &proto.GetAssignedPartitionsResponse{
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
	cache := NewTTLCache[string, *proto.GetAssignedPartitionsResponse](time.Minute)
	g := NewRingGatherer(readRing, clientPool, 2, cache, log.NewNopLogger())

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
