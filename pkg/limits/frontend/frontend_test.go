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
		numPartitions                  int
		getAssignedPartitionsResponses []*logproto.GetAssignedPartitionsResponse
		expectedStreamUsageRequest     []*logproto.GetStreamUsageRequest
		getStreamUsageResponses        []*logproto.GetStreamUsageResponse
		maxGlobalStreams               int
		ingestionRate                  float64
		expected                       []*logproto.ExceedsLimitsResult
	}{{
		name: "no streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*logproto.StreamMetadata{},
		},
		expected: nil,
	}, {
		name: "no response",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
			},
		},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{}},
		maxGlobalStreams:        10,
		ingestionRate:           100,
		expected:                nil,
	}, {
		name: "within limits",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
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
		name: "exceeds max streams limit, returns the new streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1}, // Exceeds limits.
				{StreamHash: 0x2}, // Also exceeds limits.
			},
		},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			Rate:           10,
			UnknownStreams: []uint64{0x1, 0x2},
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 0x1, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 0x2, Reason: ReasonExceedsMaxStreams},
		},
	}, {
		name: "exceeds max streams limit, allows existing streams and returns the new streams",
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
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			Rate:           10,
			UnknownStreams: []uint64{6, 7},
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 6, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 7, Reason: ReasonExceedsMaxStreams},
		},
	}, {
		// This test checks the case where a tenant's streams are sharded over
		// two instances, each holding one each stream. Each instance will
		// receive a request for just the streams in its assigned partitions.
		// The frontend is responsible for taking the union of the two responses
		// and calculating the actual set of unknown streams.
		name: "exceeds max streams limit, streams sharded over two instances",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1}, // Exceeds limits.
				{StreamHash: 0x2}, // Also exceeds limits.
			},
		},
		numPartitions: 2,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(), // Instance 0 owns partition 0.
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(), // Instance 1 owns partition 1.
			},
		}},
		// The frontend will ask instance 0 for the data for partition 0,
		// and instance 1 for the data for partition 1.
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x2},
		}, {
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  1,
			Rate:           5,
			UnknownStreams: nil,
		}, {
			// Instance 1 responds that it does not know about stream
			// 0x1. Since 0x1 shards to partition 1, and partition 1
			// is consumed by instance 1, the frontend knows that 0x1
			// is an unknown stream.
			Tenant:         "test",
			ActiveStreams:  1,
			Rate:           5,
			UnknownStreams: []uint64{0x1},
		}},
		maxGlobalStreams: 1,
		ingestionRate:    100,
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 0x1, Reason: ReasonExceedsMaxStreams},
		},
	}, {
		name: "exceeds rate limits, returns all streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x1, 0x2},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:        "test",
			ActiveStreams: 2,
			Rate:          1500, // Above the limit of 100 bytes/sec
		}},
		maxGlobalStreams: 10,
		ingestionRate:    100,
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 1, Reason: ReasonExceedsRateLimit},
			{StreamHash: 2, Reason: ReasonExceedsRateLimit},
		},
	}, {
		name: "exceeds rate limits, rates sharded over two instances",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1},
				{StreamHash: 0x2},
			},
		},
		numPartitions: 2,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}, {
			AssignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
		}},
		// The frontend will ask instance 0 for the data for partition 0,
		// and instance 1 for the data for partition 1.
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x2},
		}, {
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
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
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 1, Reason: ReasonExceedsRateLimit},
			{StreamHash: 2, Reason: ReasonExceedsRateLimit},
		},
	}, {
		name: "exceeds both max stream limit and rate limits",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{
				{StreamHash: 0x6},
				{StreamHash: 0x7},
			},
		},
		numPartitions: 1,
		getAssignedPartitionsResponses: []*logproto.GetAssignedPartitionsResponse{{
			AssignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
		}},
		expectedStreamUsageRequest: []*logproto.GetStreamUsageRequest{{
			Tenant:       "test",
			StreamHashes: []uint64{0x6, 0x7},
		}},
		getStreamUsageResponses: []*logproto.GetStreamUsageResponse{{
			Tenant:         "test",
			ActiveStreams:  5,
			UnknownStreams: []uint64{0x6, 0x7},
			Rate:           1500, // Above the limit of 100 bytes/sec
		}},
		maxGlobalStreams: 5,
		ingestionRate:    100,
		expected: []*logproto.ExceedsLimitsResult{
			{StreamHash: 0x6, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 0x7, Reason: ReasonExceedsMaxStreams},
			{StreamHash: 0x6, Reason: ReasonExceedsRateLimit},
			{StreamHash: 0x7, Reason: ReasonExceedsRateLimit},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Set up the mock clients, one for each pair of mock RPC responses.
			clients := make([]logproto.IngestLimitsClient, len(test.getAssignedPartitionsResponses))
			instances := make([]ring.InstanceDesc, len(clients))

			for i := range test.getAssignedPartitionsResponses {
				clients[i] = &mockIngestLimitsClient{
					getAssignedPartitionsResponse: test.getAssignedPartitionsResponses[i],
					expectedStreamUsageRequest:    test.expectedStreamUsageRequest[i],
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
			cache := NewNopCache[string, *logproto.GetAssignedPartitionsResponse]()

			f := Frontend{
				limits:      l,
				rateLimiter: rl,
				streamUsage: NewRingStreamUsageGatherer(readRing, clientPool, test.numPartitions, cache, log.NewNopLogger()),
				metrics:     newMetrics(prometheus.NewRegistry()),
			}

			// Set a maximum upper bound on the test execution time.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resp, err := f.ExceedsLimits(ctx, test.exceedsLimitsRequest)
			require.NoError(t, err)
			require.Equal(t, test.expected, resp.Results)
		})
	}
}
