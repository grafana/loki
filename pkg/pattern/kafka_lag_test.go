package pattern

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestLagObserver_Assign(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)
	require.Empty(t, o.partitions)

	o.Assign([]int32{0, 1})
	require.Equal(t, map[int32]time.Duration{0: 0, 1: 0}, o.partitions)
}

func TestLagObserver_Revoke(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)

	o.Assign([]int32{0, 1, 2})
	o.Revoke([]int32{0, 2})
	require.Equal(t, map[int32]time.Duration{1: 0}, o.partitions)

	// Revoking a partition that is not assigned does nothing.
	o.Revoke([]int32{3})
	require.Equal(t, map[int32]time.Duration{1: 0}, o.partitions)

	// A late record for a revoked partition does not add it back.
	o.Observe(0, time.Now())
	require.NotContains(t, o.partitions, int32(0))
}

func TestLagObserver_Observe(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)

	o.Assign([]int32{0, 1})
	o.Observe(0, time.Now().Add(-10*time.Second))
	require.GreaterOrEqual(t, o.partitions[0], 10*time.Second)
	require.Zero(t, o.partitions[1])

	// Observing a partition that is not assigned does not add it.
	o.Observe(2, time.Now())
	require.NotContains(t, o.partitions, int32(2))
}
