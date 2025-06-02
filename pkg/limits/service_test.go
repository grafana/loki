package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestService_ExceedsLimits(t *testing.T) {
	clock := quartz.NewMock(t)
	now := clock.Now()
	tests := []struct {
		name             string
		tenant           string
		activeWindow     time.Duration
		rateWindow       time.Duration
		bucketSize       time.Duration
		maxActiveStreams int
		currentUsage     []streamUsage
		request          *proto.ExceedsLimitsRequest
		expected         *proto.ExceedsLimitsResponse
		expectedEntries  int
	}{{
		// This case asserts that a tenant with no active streams is within
		// their limits.
		name:             "stream should be accepted",
		tenant:           "tenant",
		activeWindow:     DefaultActiveWindow,
		rateWindow:       DefaultRateWindow,
		bucketSize:       DefaultBucketSize,
		maxActiveStreams: 1,
		request: &proto.ExceedsLimitsRequest{
			Tenant: "tenant",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  100,
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: make([]*proto.ExceedsLimitsResult, 0),
		},
		expectedEntries: 1,
	}, {
		// This test asserts that a tenant with 1 maximum active stream can
		// push the same stream again without exceeding their limits.
		name:             "existing stream should be updated",
		tenant:           "tenant",
		activeWindow:     DefaultActiveWindow,
		rateWindow:       DefaultRateWindow,
		bucketSize:       DefaultBucketSize,
		maxActiveStreams: 1,
		currentUsage: []streamUsage{{
			hash:        0x1,
			lastSeenAt:  now.UnixNano(),
			totalSize:   100,
			rateBuckets: newRateBuckets(DefaultRateWindow, DefaultBucketSize),
		}},
		request: &proto.ExceedsLimitsRequest{
			Tenant: "tenant",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  100,
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: make([]*proto.ExceedsLimitsResult, 0),
		},
		expectedEntries: 1,
	}, {
		// This test asserts that a tenant that has reached their maximum
		// number of active streams cannot push new streams.
		name:             "new stream over limit should be rejected",
		tenant:           "tenant",
		activeWindow:     DefaultActiveWindow,
		rateWindow:       DefaultRateWindow,
		bucketSize:       DefaultBucketSize,
		maxActiveStreams: 1,
		currentUsage: []streamUsage{{
			hash:        0x1,
			lastSeenAt:  now.UnixNano(),
			totalSize:   100,
			rateBuckets: newRateBuckets(DefaultRateWindow, DefaultBucketSize),
		}},
		request: &proto.ExceedsLimitsRequest{
			Tenant: "tenant",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x2,
				TotalSize:  100,
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x2,
				Reason:     uint32(ReasonExceedsMaxStreams),
			}},
		},
		expectedEntries: 1,
	}, {
		// This test asserts that an expired stream (outside the active window)
		// is refreshed and re-used.
		name:             "expired stream should be re-refreshed",
		tenant:           "tenant",
		activeWindow:     DefaultActiveWindow,
		rateWindow:       DefaultRateWindow,
		bucketSize:       DefaultBucketSize,
		maxActiveStreams: 1,
		currentUsage: []streamUsage{{
			hash:        0x1,
			lastSeenAt:  now.Add(-DefaultActiveWindow - 1).UnixNano(),
			totalSize:   100,
			rateBuckets: newRateBuckets(DefaultRateWindow, DefaultBucketSize),
		}},
		request: &proto.ExceedsLimitsRequest{
			Tenant: "tenant",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  100,
			}},
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: make([]*proto.ExceedsLimitsResult, 0),
		},
		expectedEntries: 1,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			cfg := Config{
				ActiveWindow:  test.activeWindow,
				RateWindow:    test.rateWindow,
				BucketSize:    test.bucketSize,
				NumPartitions: 1,
			}
			limits := MockLimits{
				MaxGlobalStreams: test.maxActiveStreams,
			}
			store, err := newUsageStore(cfg.ActiveWindow, cfg.RateWindow, cfg.BucketSize, cfg.NumPartitions, reg)
			require.NoError(t, err)
			store.clock = clock
			for _, stream := range test.currentUsage {
				store.set(test.tenant, stream)
			}
			m, err := newPartitionManager(reg)
			require.NoError(t, err)
			for i := 0; i < cfg.NumPartitions; i++ {
				m.Assign([]int32{int32(i)})
			}
			m.clock = clock
			service := &Service{
				cfg:              cfg,
				limits:           &limits,
				usage:            store,
				partitionManager: m,
				producer:         newProducer(&mockKafka{}, "test", cfg.NumPartitions, "", log.NewNopLogger(), reg),
				logger:           log.NewNopLogger(),
				metrics:          newMetrics(reg),
				clock:            clock,
			}

			resp, err := service.ExceedsLimits(context.Background(), test.request)
			require.NoError(t, err)
			require.Equal(t, test.expected, resp)

			actualEntries := 0
			store.IterTenant(test.tenant, func(_ string, _ int32, _ streamUsage) {
				actualEntries++
			})
			require.Equal(t, test.expectedEntries, actualEntries)
		})
	}
}
