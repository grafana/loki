package ingester

import (
	"testing"

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
	limiter := NewLimiter(limits, NilMetrics, ring, 3)

	service := newOwnedStreamService("test", limiter)
	require.Equal(t, 0, service.getOwnedStreamCount())
	require.Equal(t, 10, service.getFixedLimit(), "fixed limit must be initialised during the instantiation")

	limits.DefaultLimits().MaxGlobalStreamsPerUser = 1000
	require.Equal(t, 10, service.getFixedLimit(), "fixed list must not be changed until update is triggered")

	service.updateFixedLimit()
	require.Equal(t, 100, service.getFixedLimit())

	service.incOwnedStreamCount()
	service.incOwnedStreamCount()
	require.Equal(t, 2, service.getOwnedStreamCount())

	service.decOwnedStreamCount()
	require.Equal(t, 1, service.getOwnedStreamCount())
}
