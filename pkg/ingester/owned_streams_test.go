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

	service.trackStreamOwnership(model.Fingerprint(1), true)
	service.trackStreamOwnership(model.Fingerprint(2), true)
	service.trackStreamOwnership(model.Fingerprint(3), true)
	require.Equal(t, 3, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)

	service.resetStreamCounts()
	service.trackStreamOwnership(model.Fingerprint(3), true)
	service.trackStreamOwnership(model.Fingerprint(3), false)
	require.Equal(t, 1, service.getOwnedStreamCount(),
		"owned streams count must not be changed because not owned stream can be reported only by recalculate_owned_streams job that resets the counters before checking all the streams")
	require.Len(t, service.notOwnedStreams, 1)
	require.True(t, service.isStreamNotOwned(model.Fingerprint(3)))

	service.resetStreamCounts()
	service.trackStreamOwnership(model.Fingerprint(1), true)
	service.trackStreamOwnership(model.Fingerprint(2), true)
	service.trackStreamOwnership(model.Fingerprint(3), false)

	service.trackRemovedStream(model.Fingerprint(3))
	require.Equal(t, 2, service.getOwnedStreamCount(), "owned stream count must be decremented only when notOwnedStream does not contain this fingerprint")
	require.Len(t, service.notOwnedStreams, 0)

	service.trackRemovedStream(model.Fingerprint(2))
	require.Equal(t, 1, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)

	group := sync.WaitGroup{}
	group.Add(100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer group.Done()
			service.trackStreamOwnership(model.Fingerprint(i+1000), true)
		}(i)
	}
	group.Wait()

	group.Add(100)
	for i := 0; i < 100; i++ {
		go func(i int) {
			defer group.Done()
			service.trackRemovedStream(model.Fingerprint(i + 1000))
		}(i)
	}
	group.Wait()

	require.Equal(t, 1, service.getOwnedStreamCount(), "owned stream count must not be changed")

	// simulate the effect from the recalculation job
	service.trackStreamOwnership(model.Fingerprint(44), false)
	service.trackStreamOwnership(model.Fingerprint(45), true)

	service.resetStreamCounts()

	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Len(t, service.notOwnedStreams, 0)
}
