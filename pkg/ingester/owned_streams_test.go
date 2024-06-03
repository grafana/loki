package ingester

import (
	"sync"
	"testing"
	"time"

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
	readRingMock := mockReadRingWithOneActiveIngester()
	limiter := NewLimiter(limits, NilMetrics, ring, 3)

	service := newOwnedStreamService("test", limiter, readRingMock, 1*time.Second)
	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Equal(t, 10, service.getFixedLimit(), "fixed limit must be initialised during the instantiation")

	limits.DefaultLimits().MaxGlobalStreamsPerUser = 1000
	require.Equal(t, 10, service.getFixedLimit(), "fixed list must not be changed until update is triggered")

	service.updateFixedLimit()
	require.Equal(t, 100, service.getFixedLimit())

	service.incOwnedStreamCount()
	service.incOwnedStreamCount()
	service.incOwnedStreamCount()
	require.Equal(t, 3, service.getOwnedStreamCount())

	// simulate the effect from the recalculation job
	service.notOwnedStreamCount = 1
	service.ownedStreamCount = 2

	service.decOwnedStreamCount()
	require.Equal(t, 2, service.getOwnedStreamCount(), "owned stream count must be decremented only when notOwnedStreamCount is set to 0")
	require.Equal(t, 0, service.notOwnedStreamCount)

	service.decOwnedStreamCount()
	require.Equal(t, 1, service.getOwnedStreamCount())
	require.Equal(t, 0, service.notOwnedStreamCount, "notOwnedStreamCount must not be decremented lower than 0")

	group := sync.WaitGroup{}
	group.Add(200)
	for i := 0; i < 100; i++ {
		go func() {
			defer group.Done()
			service.incOwnedStreamCount()
		}()
	}

	for i := 0; i < 100; i++ {
		go func() {
			defer group.Done()
			service.decOwnedStreamCount()
		}()
	}
	group.Wait()

	require.Equal(t, 1, service.getOwnedStreamCount(), "owned stream count must not be changed")
}
