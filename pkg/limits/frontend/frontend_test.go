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
	f := newTestFrontend(t)
	f.limitsClient = &mockLimitsClient{t: t, err: errors.New("boom")}
	req := &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
	}
	resp, err := f.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []*proto.StreamShardResult{{
		StreamHash:           0x1,
		Shards:               1,
		ShardDecisionContext: uint32(limits.ReasonFailed),
	}}, resp.Results)
}

func TestFrontend_CheckLimitsAndShard_NoCaching(t *testing.T) {
	// Regression guard: there is no fail-open cache. A real decision from
	// one call must never be remembered and replayed on a later failure --
	// every total failure collapses to 1 shard, and every per-stream
	// failure/rejection is passed straight through from the backend/ring,
	// regardless of what any earlier call returned for the same stream.
	f := newTestFrontend(t)
	req := &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
	}

	mockClient := &mockLimitsClient{
		t: t,
		checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
			Results: []*proto.StreamShardResult{{StreamHash: 0x1, Shards: 20}},
		},
	}
	f.limitsClient = mockClient
	resp, err := f.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, uint32(20), resp.Results[0].Shards)

	// A per-stream failure placeholder (RPC succeeds overall, but this
	// stream's own partition went unanswered) passes through as 1 shard,
	// not the earlier 20.
	mockClient.checkLimitsAndShardResponse = &proto.CheckLimitsAndShardResponse{
		Results: []*proto.StreamShardResult{{
			StreamHash:           0x1,
			Shards:               1,
			ShardDecisionContext: uint32(limits.ReasonFailed),
		}},
	}
	resp, err = f.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, uint32(1), resp.Results[0].Shards)

	// A total RPC failure also collapses to 1 shard, not the earlier 20.
	mockClient.err = errors.New("boom")
	resp, err = f.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, uint32(1), resp.Results[0].Shards)
}
