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
	// Whatever the failure, the stream degrades to "don't shard this push"
	// (one shard) rather than being rejected.
	expected := []*proto.StreamShardResult{{
		StreamHash: 0x1,
		Shards:     1,
		Stats:      &proto.ShardStats{ShardDecisionContext: uint32(limits.ReasonFailed)},
	}}

	// The whole client call fails (e.g. the ring could not be queried): every
	// stream fails open.
	t.Run("client call fails", func(t *testing.T) {
		f := newTestFrontend(t)
		f.limitsClient = &mockLimitsClient{t: t, err: errors.New("boom")}
		resp, err := f.CheckLimitsAndShard(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, expected, resp.Results)
	})

	// The RPC to the backend instance owning the stream's partition returns an
	// error. The error is swallowed and the stream, left unanswered after all
	// zones are exhausted, fails open.
	t.Run("backend rpc fails", func(t *testing.T) {
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
