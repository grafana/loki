package ingester

import (
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

func Test_OwnedStreamService(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxGlobalStreamsPerUser: 100,
	}, nil)
	require.NoError(t, err)
	// Mock the ring
	ring := &ringCountMock{count: 30}
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(ring, 3), &TenantBasedStrategy{limits: limits})

	service := newOwnedStreamService("test", limiter)
	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Equal(t, 10, service.getFixedLimit(), "fixed limit must be initialised during the instantiation")

	limits.DefaultLimits().MaxGlobalStreamsPerUser = 1000
	require.Equal(t, 10, service.getFixedLimit(), "fixed list must not be changed until update is triggered")

	service.updateFixedLimit()
	require.Equal(t, 100, service.getFixedLimit())

	service.trackStreamOwnership(model.Fingerprint(1), true, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(2), true, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(3), true, noPolicy)
	require.Equal(t, 3, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)

	service.resetStreamCounts()
	service.trackStreamOwnership(model.Fingerprint(3), true, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(3), false, noPolicy)
	require.Equal(t, 1, service.getOwnedStreamCount(),
		"owned streams count must not be changed because not owned stream can be reported only by recalculate_owned_streams job that resets the counters before checking all the streams")
	require.Len(t, service.notOwnedStreams, 1)
	require.True(t, service.isStreamNotOwned(model.Fingerprint(3)))

	service.resetStreamCounts()
	service.trackStreamOwnership(model.Fingerprint(1), true, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(2), true, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(3), false, noPolicy)

	service.trackRemovedStream(model.Fingerprint(3), noPolicy)
	require.Equal(t, 2, service.getOwnedStreamCount(), "owned stream count must be decremented only when notOwnedStream does not contain this fingerprint")
	require.Len(t, service.notOwnedStreams, 0)

	service.trackRemovedStream(model.Fingerprint(2), noPolicy)
	require.Equal(t, 1, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)

	group := sync.WaitGroup{}
	group.Add(100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer group.Done()
			service.trackStreamOwnership(model.Fingerprint(i+1000), true, noPolicy)
		}(i)
	}
	group.Wait()

	group.Add(100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer group.Done()
			service.trackRemovedStream(model.Fingerprint(i+1000), noPolicy)
		}(i)
	}
	group.Wait()

	require.Equal(t, 1, service.getOwnedStreamCount(), "owned stream count must not be changed")

	// simulate the effect from the recalculation job
	service.trackStreamOwnership(model.Fingerprint(44), false, noPolicy)
	service.trackStreamOwnership(model.Fingerprint(45), true, noPolicy)

	service.resetStreamCounts()

	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)
}

func Test_OwnedStreamService_PolicyStreamCounting(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxGlobalStreamsPerUser: 100,
	}, nil)
	require.NoError(t, err)

	ring := &ringCountMock{count: 30}
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(ring, 3), &TenantBasedStrategy{limits: limits})

	service := newOwnedStreamService("test", limiter)

	// Initially no streams
	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Equal(t, 0, service.getPolicyStreamCount("finance"))
	require.Equal(t, 0, service.getPolicyStreamCount("ops"))

	// Track streams with different policies
	service.trackStreamOwnership(model.Fingerprint(1), true, "finance")
	service.trackStreamOwnership(model.Fingerprint(2), true, "finance")
	service.trackStreamOwnership(model.Fingerprint(3), true, "ops")
	service.trackStreamOwnership(model.Fingerprint(4), true, noPolicy) // no policy

	// Verify counts
	require.Equal(t, 4, service.getOwnedStreamCount())               // total streams
	require.Equal(t, 2, service.getPolicyStreamCount("finance"))     // finance policy streams
	require.Equal(t, 1, service.getPolicyStreamCount("ops"))         // ops policy streams
	require.Equal(t, 0, service.getPolicyStreamCount("nonexistent")) // non-existent policy
	require.Equal(t, 2, service.getActivePolicyCount())              // finance and ops policies

	// Remove streams
	service.trackRemovedStream(model.Fingerprint(1), "finance")
	service.trackRemovedStream(model.Fingerprint(3), "ops")

	// Verify updated counts
	require.Equal(t, 2, service.getOwnedStreamCount())           // total streams
	require.Equal(t, 1, service.getPolicyStreamCount("finance")) // finance policy streams
	require.Equal(t, 0, service.getPolicyStreamCount("ops"))     // ops policy streams
	require.Equal(t, 1, service.getActivePolicyCount())          // only finance policy remains

	// Reset and verify
	service.resetStreamCounts()
	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Equal(t, 0, service.getPolicyStreamCount("finance"))
	require.Equal(t, 0, service.getPolicyStreamCount("ops"))
}

func Test_OwnedStreamService_PolicyCleanup(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxGlobalStreamsPerUser: 100,
	}, nil)
	require.NoError(t, err)

	ring := &ringCountMock{count: 30}
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(ring, 3), &TenantBasedStrategy{limits: limits})

	service := newOwnedStreamService("test", limiter)

	// Initially no policies
	require.Equal(t, 0, service.getActivePolicyCount())

	// Add streams with different policies
	service.trackStreamOwnership(model.Fingerprint(1), true, "finance")
	service.trackStreamOwnership(model.Fingerprint(2), true, "finance")
	service.trackStreamOwnership(model.Fingerprint(3), true, "ops")
	service.trackStreamOwnership(model.Fingerprint(4), true, "dev")

	// Verify we have 3 active policies
	require.Equal(t, 3, service.getActivePolicyCount())
	require.Equal(t, 2, service.getPolicyStreamCount("finance"))
	require.Equal(t, 1, service.getPolicyStreamCount("ops"))
	require.Equal(t, 1, service.getPolicyStreamCount("dev"))

	// Remove all finance streams - should clean up the policy
	service.trackRemovedStream(model.Fingerprint(1), "finance")
	service.trackRemovedStream(model.Fingerprint(2), "finance")

	// Verify finance policy is cleaned up
	require.Equal(t, 2, service.getActivePolicyCount()) // finance removed, ops and dev remain
	require.Equal(t, 0, service.getPolicyStreamCount("finance"))
	require.Equal(t, 1, service.getPolicyStreamCount("ops"))
	require.Equal(t, 1, service.getPolicyStreamCount("dev"))

	// Remove ops stream - should clean up the policy
	service.trackRemovedStream(model.Fingerprint(3), "ops")

	// Verify ops policy is cleaned up
	require.Equal(t, 1, service.getActivePolicyCount()) // only dev remains
	require.Equal(t, 0, service.getPolicyStreamCount("ops"))
	require.Equal(t, 1, service.getPolicyStreamCount("dev"))

	// Remove dev stream - should clean up the policy
	service.trackRemovedStream(model.Fingerprint(4), "dev")

	// Verify dev policy is cleaned up
	require.Equal(t, 0, service.getActivePolicyCount()) // no policies left
	require.Equal(t, 0, service.getPolicyStreamCount("dev"))

	// Verify total stream count is correct
	require.Equal(t, 0, service.getOwnedStreamCount())
}
