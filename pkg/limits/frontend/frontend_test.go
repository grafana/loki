package frontend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestFrontend_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name                  string
		exceedsLimitsRequest  *proto.ExceedsLimitsRequest
		exceedsLimitsResponse *proto.ExceedsLimitsResponse
		err                   error
		expected              *proto.ExceedsLimitsResponse
	}{{
		name: "when the request contains no streams, the response is success",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: nil,
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{},
		expected:              &proto.ExceedsLimitsResponse{},
	}, {
		name: "request contains one stream, the response is success",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{},
		expected:              &proto.ExceedsLimitsResponse{},
	}, {
		name: "request contains two streams, the response is success",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x4,
				TotalSize:  0x9,
			}},
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{},
		expected:              &proto.ExceedsLimitsResponse{},
	}, {
		name: "request contains one stream over the stream limit",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		name: "request contains two streams over the stream limit",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x4,
				TotalSize:  0x9,
			}},
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		name: "request contains two streams, but just one stream is over the stream limit",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x4,
				TotalSize:  0x9,
			}},
		},
		exceedsLimitsResponse: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}, {
		name: "unexpected error, response with failed reason",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x2,
				TotalSize:  0x9,
			}},
		},
		err: errors.New("an unexpected error occurred"),
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonFailed),
			}, {
				StreamHash: 0x2,
				Reason:     uint32(limits.ReasonFailed),
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readRing, _ := newMockRingWithClientPool(t, "test", nil, nil)
			f, err := New(Config{
				LifecyclerConfig: ring.LifecyclerConfig{
					RingConfig: ring.Config{
						KVStore: kv.Config{
							Store: "inmemory",
						},
					},
					HeartbeatPeriod:  time.Second,
					HeartbeatTimeout: time.Minute,
				},
			}, "test", readRing, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)
			// Replace with our mock.
			f.limitsClient = &mockLimitsClient{
				t:                            t,
				expectedExceedsLimitsRequest: test.exceedsLimitsRequest,
				exceedsLimitsResponse:        test.exceedsLimitsResponse,
				err:                          test.err,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			actual, err := f.ExceedsLimits(ctx, test.exceedsLimitsRequest)
			require.NoError(t, err)
			require.Equal(t, test.expected, actual)
		})
	}
}

func newTestFrontend(t *testing.T) *Frontend {
	t.Helper()
	readRing, _ := newMockRingWithClientPool(t, "test", nil, nil)
	f, err := New(Config{
		LifecyclerConfig: ring.LifecyclerConfig{
			RingConfig: ring.Config{
				KVStore: kv.Config{
					Store: "inmemory",
				},
			},
			HeartbeatPeriod:  time.Second,
			HeartbeatTimeout: time.Minute,
		},
	}, "test", readRing, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return f
}

func TestFrontend_CheckLimitsAndShard_FailsOpenToOneShard(t *testing.T) {
	req := &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
	}
	expected := []*proto.StreamShardResult{{
		StreamHash: 0x1,
		Shards:     1,
		Stats:      &proto.ShardStats{ShardDecisionContext: uint32(limits.ReasonFailed)},
	}}

	t.Run("the whole client call fails, for instance because the ring could not be queried", func(t *testing.T) {
		f := newTestFrontend(t)
		f.limitsClient = &mockLimitsClient{t: t, err: errors.New("boom")}
		resp, err := f.CheckLimitsAndShard(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, expected, resp.Results)
	})

	t.Run("the backend consuming the stream's partition returns an error, leaving the stream unanswered", func(t *testing.T) {
		instances := []ring.InstanceDesc{{Addr: "instance-0"}}
		mockClient := &mockLimitsProtoClient{
			t: t,
			getAssignedPartitionsResponse: &proto.GetAssignedPartitionsResponse{
				AssignedPartitions: map[int32]int64{0: time.Now().UnixNano()},
			},
			expectedNumAssignedPartitionsRequests:  1,
			checkLimitsAndShardResponseErr:         errors.New("boom"),
			expectedNumCheckLimitsAndShardRequests: 1,
		}
		t.Cleanup(mockClient.Finished)
		readRing, clientPool := newMockRingWithClientPool(t, "test", []*mockLimitsProtoClient{mockClient}, instances)
		cache := newNopCache[string, *proto.GetAssignedPartitionsResponse]()

		f := newTestFrontend(t)
		f.limitsClient = newRingLimitsClient(readRing, clientPool, 1, cache, log.NewNopLogger(), prometheus.NewRegistry())
		resp, err := f.CheckLimitsAndShard(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, expected, resp.Results)
	})
}

func TestFrontend_CheckLimitsAndShard_CompletesPartialResponses(t *testing.T) {
	streams := []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}}
	unsharded := &proto.StreamShardResult{
		StreamHash: 0x1,
		Shards:     1,
		Stats:      &proto.ShardStats{EvaluatedRate: 0x10},
	}
	sharded := &proto.StreamShardResult{
		StreamHash: 0x2,
		Shards:     4,
		Stats:      &proto.ShardStats{EvaluatedRate: 0x100},
	}
	rejected := &proto.StreamShardResult{
		StreamHash:   0x1,
		Shards:       0,
		RejectReason: limits.ReasonMaxStreams.String(),
	}
	failedOpen := func(streamHash uint64) *proto.StreamShardResult {
		return &proto.StreamShardResult{
			StreamHash: streamHash,
			Shards:     1,
			Stats:      &proto.ShardStats{ShardDecisionContext: uint32(limits.ReasonFailed)},
		}
	}

	tests := []struct {
		name             string
		response         *proto.CheckLimitsAndShardResponse
		expected         []*proto.StreamShardResult
		expectedShards   float64
		expectedFailed   float64
		expectedRejected float64
	}{{
		name:           "no results, as returned until the decision logic lands",
		response:       &proto.CheckLimitsAndShardResponse{},
		expected:       []*proto.StreamShardResult{failedOpen(0x1), failedOpen(0x2)},
		expectedShards: 2,
		expectedFailed: 2,
	}, {
		name:           "results for a subset of the streams, as when no instance owns a partition",
		response:       &proto.CheckLimitsAndShardResponse{Results: []*proto.StreamShardResult{sharded}},
		expected:       []*proto.StreamShardResult{sharded, failedOpen(0x1)},
		expectedShards: 5,
		expectedFailed: 1,
	}, {
		name:           "results for all streams",
		response:       &proto.CheckLimitsAndShardResponse{Results: []*proto.StreamShardResult{unsharded, sharded}},
		expected:       []*proto.StreamShardResult{unsharded, sharded},
		expectedShards: 5,
	}, {
		name: "a shard count of zero without a rejection carries no decision",
		response: &proto.CheckLimitsAndShardResponse{Results: []*proto.StreamShardResult{{
			StreamHash: 0x1,
			Shards:     0,
			Stats:      &proto.ShardStats{EvaluatedRate: 0x10},
		}, sharded}},
		expected:       []*proto.StreamShardResult{failedOpen(0x1), sharded},
		expectedShards: 5,
		expectedFailed: 1,
	}, {
		name:             "a rejected stream keeps its zero shard count",
		response:         &proto.CheckLimitsAndShardResponse{Results: []*proto.StreamShardResult{rejected, sharded}},
		expected:         []*proto.StreamShardResult{rejected, sharded},
		expectedShards:   4,
		expectedRejected: 1,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newTestFrontend(t)
			f.limitsClient = &mockLimitsClient{t: t, checkLimitsAndShardResponse: test.response}
			resp, err := f.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: streams,
			})
			require.NoError(t, err)
			require.Equal(t, test.expected, resp.Results)
			require.Equal(t, float64(len(streams)), testutil.ToFloat64(f.checkLimitsAndShardStreams.WithLabelValues("test")))
			require.Equal(t, test.expectedShards, testutil.ToFloat64(f.checkLimitsAndShardShards.WithLabelValues("test")))
			require.Equal(t, test.expectedFailed, testutil.ToFloat64(f.checkLimitsAndShardFailed.WithLabelValues("test")))
			require.Equal(t, test.expectedRejected, testutil.ToFloat64(f.checkLimitsAndShardRejected.WithLabelValues("test")))
		})
	}
}
