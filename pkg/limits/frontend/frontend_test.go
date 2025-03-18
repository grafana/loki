package frontend

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFrontend_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name                           string
		exceedsLimitsRequest           *logproto.ExceedsLimitsRequest
		getAssignedPartitionsResponses []*logproto.GetAssignedPartitionsResponse
		expectedStreamUsageRequest     []*logproto.GetStreamUsageRequest
		getStreamUsageResponses        []*logproto.GetStreamUsageResponse
		maxGlobalStreams               int
		ingestionRate                  float64
		expected                       []*logproto.RejectedStream
	}{{
		name: "no streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*logproto.StreamMetadata{},
		},
		expected: nil,
	}, {
		name: "below the limit",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
			Partitions:   []int32{0},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 2,
			Rate:          10,
		}},
		maxGlobalStreams: 10,
		ingestionRate:    100,
		expected:         nil,
	}, {
		name: "exceeds limit with new streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1}, // Exceeds limits.
				{StreamHash: 0x2}, // Also exceeds limits.
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
			Partitions:   []int32{0},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			Rate:           10,
			UnknownStreams: []uint64{0x1, 0x2},
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.RejectedStream{
			{StreamHash: 0x1, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 0x2, Reason: ReasonExceedsMaxStreams},
		},
	}, {
		name: "exceeds limit but allows existing streams and rejects new streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
				{StreamHash: 0x3},
				{StreamHash: 0x4},
				{StreamHash: 0x5},
				{StreamHash: 0x6}, // Exceeds limits.
				{StreamHash: 0x7}, // Also exceeds limits.
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7},
			Partitions:   []int32{0},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			Rate:           10,
			UnknownStreams: []uint64{6, 7},
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.RejectedStream{
			{StreamHash: 6, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 7, Reason: ReasonExceedsMaxStreams},
		},
	}, {
		name: "no response",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
			Partitions:   []int32{0},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{}},
		maxGlobalStreams:        10,
		ingestionRate:           100,
		expected:                nil, // No rejections because activeStreamsTotal is 0
	}, {
		name: "rate limit not exceeded",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
			Partitions:   []int32{0},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 2,
			Rate:          50, // Below the limit of 100 bytes/sec
		}},
		maxGlobalStreams: 10,
		ingestionRate:    100,
		expected:         nil,
	}, {
		name: "rate limit exceeded",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 2,
			Rate:          1500, // Above the limit of 100 bytes/sec
		}},
		maxGlobalStreams: 10,
		ingestionRate:    100,
		expected: []*logproto.RejectedStream{
			{StreamHash: 1, Reason: ReasonExceedsRateLimit},
			{StreamHash: 2, Reason: ReasonExceedsRateLimit},
		},
	}, {
		name: "rate limit exceeded with multiple instances",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: 1,
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: 1,
			},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          600,
		}, {
			Tenant:        "test",
			ActiveStreams: 1,
			Rate:          500,
		}},
		maxGlobalStreams: 10,
		ingestionRate:    100,
		expected: []*logproto.RejectedStream{
			{StreamHash: 1, Reason: ReasonExceedsRateLimit},
			{StreamHash: 2, Reason: ReasonExceedsRateLimit},
		},
	}, {
		name: "both global limit and rate limit exceeded",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x6},
				{StreamHash: 0x7},
			},
		},
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: 1,
			},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			UnknownStreams: []uint64{0x6, 0x7},
			Rate:           1500, // Above the limit of 100 bytes/sec
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.RejectedStream{
			{StreamHash: 0x6, Reason: ReasonExceedsRateLimit},
			{StreamHash: 0x7, Reason: ReasonExceedsRateLimit},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			clients := make([]logproto.IngestLimitsClient, len(test.getAssignedPartitionsResponses))
			instances := make([]ring.InstanceDesc, len(clients))

			for i := 0; i < len(test.getAssignedPartitionsResponses); i++ {
				clients[i] = &mockIngestLimitsClient{
					getAssignedPartitionsResponse: test.getAssignedPartitionsResponses[i],
					getStreamUsageResponse:        test.getStreamUsageResponses[i],
					t:                             t,
				}
				instances[i] = ring.InstanceDesc{
					Addr: fmt.Sprintf("instance-%d", i),
				}
			}

			// Set up the mocked ring and client pool for the tests.
			readRing, clientPool := newMockRingWithClientPool(t, "test", clients, instances)
			l := &mockLimits{
				maxGlobalStreams: test.maxGlobalStreams,
				ingestionRate:    test.ingestionRate,
			}
			rl := limiter.NewRateLimiter(newRateLimitsAdapter(l), 10*time.Second)

			f := Frontend{
				limits:      l,
				rateLimiter: rl,
				streamUsage: NewRingStreamUsageGatherer(readRing, clientPool, log.NewNopLogger()),
				metrics:     newMetrics(prometheus.NewRegistry()),
			}

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resp, err := f.ExceedsLimits(ctx, test.exceedsLimitsRequest)
			require.NoError(t, err)
			require.Equal(t, test.expected, resp.RejectedStreams)
		})
	}
}
