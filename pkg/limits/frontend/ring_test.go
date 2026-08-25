package frontend

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestRingLimitsClient_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name    string
		request *proto.ExceedsLimitsRequest
		// Instances contains the complete set of instances that should be
		// mocked. For example, if a test case is expected to make RPC calls
		// to one instance, then just one InstanceDesc is required.
		instances     []ring.InstanceDesc
		numPartitions int
		// The following request and response slices contain the expected
		// RPCs for each instance, and MUST be the same size as the instances
		// slice (otherwise the test will panic).
		getAssignedPartitionsResponses    []*proto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs []error
		expectedExceedsLimitsRequests     []*proto.ExceedsLimitsRequest
		exceedsLimitsResponses            []*proto.ExceedsLimitsResponse
		exceedsLimitsResponseErrs         []error
		// The results are sorted by StreamHash before asserting against the
		// expected results. [ExceedsLimits] does not guarantee that results
		// are returned in a deterministic order as sorting is expensive.
		expected    *proto.ExceedsLimitsResponse
		expectedErr string
	}{{
		// When there are no streams, no RPCs should be sent.
		name: "no streams",
		request: &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: nil,
		},
		instances:                         []ring.InstanceDesc{{Addr: "instance-0"}},
		numPartitions:                     1,
		getAssignedPartitionsResponses:    []*proto.GetAssignedPartitionsResponse{nil},
		getAssignedPartitionsResponseErrs: []error{nil},
		expectedExceedsLimitsRequests:     []*proto.ExceedsLimitsRequest{nil},
		exceedsLimitsResponses:            []*proto.ExceedsLimitsResponse{nil},
		exceedsLimitsResponseErrs:         []error{nil},
		expected:                          &proto.ExceedsLimitsResponse{},
	}, {
		// When there is one instance owning all partitions, that instance is
		// responsible for enforcing limits of all streams.
		name: "one stream one instance",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 0.
				TotalSize:  0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getAssignedPartitionsResponseErrs: []error{nil},
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		// When there are two instances, each instance is responsible for
		// enforcing limits on just the streams that shard to its consumed
		// partitions. But when we have one stream, just one instance
		// should be called to enforce limits.
		name: "one stream two instances",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 1.
				TotalSize:  0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions: 2,
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
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{
			nil, {
				Tenant: "test",
				Streams: []*proto.StreamMetadata{{
					StreamHash: 0x1,
					TotalSize:  0x5,
				}},
			},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{
			nil, {
				Results: []*proto.ExceedsLimitsResult{{
					StreamHash: 0x1,
					Reason:     uint32(limits.ReasonMaxStreams),
				}},
			},
		},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		// When there are two streams and two instances, but all streams
		// shard to one partition, just the instance that consumes that
		// partition should be called to enforce limits.
		name: "two streams, two instances, all streams to one partition",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 1.
				TotalSize:  0x5,
			}, {
				StreamHash: 0x3, // 0x3 is also assigned to partition 1.
				TotalSize:  0x9,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions: 2,
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
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{
			nil, {
				Tenant: "test",
				Streams: []*proto.StreamMetadata{{
					StreamHash: 0x1,
					TotalSize:  0x5,
				}, {
					StreamHash: 0x3,
					TotalSize:  0x9,
				}},
			},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{
			nil, {
				Results: []*proto.ExceedsLimitsResult{{
					StreamHash: 0x1,
					Reason:     uint32(limits.ReasonMaxStreams),
				}},
			},
		},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		// When there are two streams and two instances, and each stream
		// shards to different partitions, all instances should be called
		// called to enforce limits.
		name: "two streams, two instances, one stream each",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 1.
				TotalSize:  0x5,
			}, {
				StreamHash: 0x2, // 0x2 is also assigned to partition 0.
				TotalSize:  0x9,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions: 2,
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
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x2,
				TotalSize:  0x9,
			}},
		}, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{nil, nil},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}, {
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		// When one instance returns an error, the streams for that instance
		// are failed.
		name: "two streams, two instances, one instance returns error",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 1.
				TotalSize:  0x5,
			}, {
				StreamHash: 0x2, // 0x2 is also assigned to partition 0.
				TotalSize:  0x9,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}, {
			Addr: "instance-1",
		}},
		numPartitions: 2,
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
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x2,
				TotalSize:  0x9,
			}},
		}, {
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}, nil},
		exceedsLimitsResponseErrs: []error{nil, errors.New("an unexpected error occurred")},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonFailed),
			}, {
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		// When one zone returns an error, the streams for that instance
		// should be checked against the other zone.
		name: "two streams, two zones, one instance each, first zone returns error",
		request: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1, // 0x1 is assigned to partition 1.
				TotalSize:  0x5,
			}},
		},
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}, {
			Addr: "instance-b-0",
			Zone: "b",
		}},
		numPartitions: 1,
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
		expectedExceedsLimitsRequests: []*proto.ExceedsLimitsRequest{{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		}, {
			// The instance in zone b should receive the same request as the
			// instance in zone a.
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		}},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{nil, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}},
		exceedsLimitsResponseErrs: []error{errors.New("an unexpected error occurred"), nil},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each set of mock RPC responses.
			mockClients := make([]*mockLimitsProtoClient, len(test.instances))
			for i := 0; i < len(test.instances); i++ {
				expectedNumAssignedPartitionsRequests := 0
				if test.getAssignedPartitionsResponses[i] != nil {
					expectedNumAssignedPartitionsRequests = 1
				}
				expectedNumExceedsLimitsRequests := 0
				if test.expectedExceedsLimitsRequests[i] != nil {
					expectedNumExceedsLimitsRequests = 1
				}
				mockClients[i] = &mockLimitsProtoClient{
					t:                                     t,
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:      test.getAssignedPartitionsResponseErrs[i],
					expectedExceedsLimitsRequest:          test.expectedExceedsLimitsRequests[i],
					exceedsLimitsResponse:                 test.exceedsLimitsResponses[i],
					exceedsLimitsResponseErr:              test.exceedsLimitsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: expectedNumAssignedPartitionsRequests,
					expectedNumExceedsLimitsRequests:      expectedNumExceedsLimitsRequests,
				}
				t.Cleanup(mockClients[i].Finished)
			}
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := newNopCache[string, *proto.GetAssignedPartitionsResponse]()
			r := newRingLimitsClient(readRing, clientPool, test.numPartitions, cache, log.NewNopLogger(), prometheus.NewRegistry())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			actual, err := r.ExceedsLimits(ctx, test.request)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				require.Nil(t, actual)
			} else {
				require.NoError(t, err)
				slices.SortFunc(actual.Results, func(a, b *proto.ExceedsLimitsResult) int {
					if a.StreamHash < b.StreamHash {
						return -1
					} else if a.StreamHash == b.StreamHash {
						return 0
					} else { //nolint:revive
						return 1
					}
				})
				require.Equal(t, test.expected, actual)
			}
		})
	}
}

func TestRingLimitsClient_GetZoneAwarePartitionConsumers(t *testing.T) {
	tests := []struct {
		name                              string
		instances                         []ring.InstanceDesc
		getAssignedPartitionsResponses    []*proto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs []error
		expected                          map[string]map[int32]string
	}{{
		name: "single zone",
		instances: []ring.InstanceDesc{{
			Addr: "instance-a-0",
			Zone: "a",
		}},
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
		getAssignedPartitionsResponses:    []*proto.GetAssignedPartitionsResponse{{}, {}},
		getAssignedPartitionsResponseErrs: []error{nil, nil, nil},
		expected:                          map[string]map[int32]string{"a": {}, "b": {}},
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
			mockClients := make([]*mockLimitsProtoClient, len(test.instances))
			for i := range test.instances {
				// These test cases assume one request/response per instance.
				mockClients[i] = &mockLimitsProtoClient{
					t:                                     t,
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:      test.getAssignedPartitionsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: 1,
				}
				t.Cleanup(mockClients[i].Finished)
			}
			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := newNopCache[string, *proto.GetAssignedPartitionsResponse]()
			r := newRingLimitsClient(readRing, clientPool, 2, cache, log.NewNopLogger(), prometheus.NewRegistry())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result, err := r.getZoneAwarePartitionConsumers(ctx, test.instances)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRingLimitsClient_GetPartitionConsumers(t *testing.T) {
	tests := []struct {
		name string
		// Instances contains the complete set of instances that should be
		// mocked. For example, if a test case is expected to make RPC calls
		// to one instance, then just one InstanceDesc is required.
		instances []ring.InstanceDesc
		// The following request and response slices contain the expected
		// RPCs for each instance, and MUST be the same size as the instances
		// slice (otherwise the test will panic).
		getAssignedPartitionsResponses    []*proto.GetAssignedPartitionsResponse
		getAssignedPartitionsResponseErrs []error
		// The expected result.
		expected map[int32]string
	}{{
		name: "single instance returns its partitions",
		instances: []ring.InstanceDesc{{
			Addr: "instance-0",
		}},
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
		getAssignedPartitionsResponses: []*proto.GetAssignedPartitionsResponse{nil, nil},
		getAssignedPartitionsResponseErrs: []error{
			errors.New("an unexpected error occurred"),
			errors.New("an unexpected error occurred"),
		},
		expected: map[int32]string{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			mockClients := make([]*mockLimitsProtoClient, len(test.instances))
			for i := range test.instances {
				// These test cases assume one request/response per instance.
				mockClients[i] = &mockLimitsProtoClient{
					t:                                     t,
					getAssignedPartitionsResponse:         test.getAssignedPartitionsResponses[i],
					getAssignedPartitionsResponseErr:      test.getAssignedPartitionsResponseErrs[i],
					expectedNumAssignedPartitionsRequests: 1,
				}
				t.Cleanup(mockClients[i].Finished)
			}
			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, test.instances)
			cache := newNopCache[string, *proto.GetAssignedPartitionsResponse]()
			r := newRingLimitsClient(readRing, clientPool, 1, cache, log.NewNopLogger(), prometheus.NewRegistry())

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			result, err := r.getPartitionConsumers(ctx, test.instances)
			require.NoError(t, err)
			require.Equal(t, test.expected, result)
		})
	}
}

func TestRingLimitsClient_GetPartitionConsumers_Caching(t *testing.T) {
	// Set up the mock clients.
	client0 := mockLimitsProtoClient{
		t: t,
		getAssignedPartitionsResponse: &proto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		},
		// Expect the same request twice. The first time on the first
		// cache miss, and the second time on the second cache miss after
		// resetting the cache.
		expectedNumAssignedPartitionsRequests: 2,
	}
	t.Cleanup(client0.Finished)
	client1 := mockLimitsProtoClient{
		t: t,
		getAssignedPartitionsResponse: &proto.GetAssignedPartitionsResponse{
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		},
		// Expect the same request twice too for the same reasons.
		expectedNumAssignedPartitionsRequests: 2,
	}
	t.Cleanup(client1.Finished)
	mockClients := []*mockLimitsProtoClient{&client0, &client1}
	instances := []ring.InstanceDesc{{Addr: "instance-0"}, {Addr: "instance-1"}}

	// Set up the mocked ring and client pool for the tests.
	readRing, clientPool := newMockRingWithClientPool(t, "test", mockClients, instances)

	// Set the cache TTL large enough that entries cannot expire (flake)
	// during slow test runs.
	cache := newTTLCache[string, *proto.GetAssignedPartitionsResponse](time.Minute)
	r := newRingLimitsClient(readRing, clientPool, 2, cache, log.NewNopLogger(), prometheus.NewRegistry())

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
	actual, err := r.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client0.numAssignedPartitionsRequests)
	require.Equal(t, 1, client1.numAssignedPartitionsRequests)

	// The second call should be a cache hit.
	actual, err = r.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 1, client0.numAssignedPartitionsRequests)
	require.Equal(t, 1, client1.numAssignedPartitionsRequests)

	// Expire the cache, it should be a cache miss.
	cache.Reset()

	// The third call should be a cache miss.
	actual, err = r.getPartitionConsumers(ctx, instances)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, 2, client0.numAssignedPartitionsRequests)
	require.Equal(t, 2, client1.numAssignedPartitionsRequests)
}
