package pattern

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestLagObserver_Assign(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)
	require.Empty(t, o.partitions)

	o.Assign([]int32{0, 1})
	require.Equal(t, map[int32]lagObserverEntry{0: {}, 1: {}}, o.partitions)
}

func TestLagObserver_Revoke(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)

	o.Assign([]int32{0, 1, 2})
	o.Revoke([]int32{0, 2})
	require.Equal(t, map[int32]lagObserverEntry{1: {}}, o.partitions)

	// Revoking a partition that is not assigned does nothing.
	o.Revoke([]int32{3})
	require.Equal(t, map[int32]lagObserverEntry{1: {}}, o.partitions)

	// A late record for a revoked partition does not add it back.
	o.Observe(0, time.Now())
	require.NotContains(t, o.partitions, int32(0))
}

func TestLagObserver_Observe(t *testing.T) {
	o, err := newLagObserver(prometheus.NewRegistry())
	require.NoError(t, err)

	o.Assign([]int32{0, 1})
	o.Observe(0, time.Now().Add(-10*time.Second))
	require.GreaterOrEqual(t, o.partitions[0].lag, 10*time.Second)
	require.False(t, o.partitions[0].observedAt.IsZero())
	require.Zero(t, o.partitions[1])

	// Observing a partition that is not assigned does not add it.
	o.Observe(2, time.Now())
	require.NotContains(t, o.partitions, int32(2))
}

func TestLagObserver_Collect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o, err := newLagObserver(prometheus.NewRegistry())
		require.NoError(t, err)

		o.Assign([]int32{0, 1})
		o.Observe(0, time.Now().Add(-10*time.Second))
		require.NoError(t, testutil.CollectAndCompare(o, strings.NewReader(`
# HELP consumer_lag_seconds The consumer lag of each assigned partition, measured as the age of the last record consumed from Kafka.
# TYPE consumer_lag_seconds gauge
consumer_lag_seconds{partition="0"} 10
consumer_lag_seconds{partition="1"} 0
`)))

		// Nothing should be reset until 1 minute has elapsed.
		time.Sleep(time.Minute)
		require.NoError(t, testutil.CollectAndCompare(o, strings.NewReader(`
# HELP consumer_lag_seconds The consumer lag of each assigned partition, measured as the age of the last record consumed from Kafka.
# TYPE consumer_lag_seconds gauge
consumer_lag_seconds{partition="0"} 10
consumer_lag_seconds{partition="1"} 0
`)))

		// More than one minute has passed, and no partitions have recent observations,
		// their lag should be reset to 0.
		time.Sleep(time.Nanosecond)
		require.NoError(t, testutil.CollectAndCompare(o, strings.NewReader(`
# HELP consumer_lag_seconds The consumer lag of each assigned partition, measured as the age of the last record consumed from Kafka.
# TYPE consumer_lag_seconds gauge
consumer_lag_seconds{partition="0"} 0
consumer_lag_seconds{partition="1"} 0
`)))

		// A new observation reports the lag again.
		o.Observe(0, time.Now().Add(-5*time.Second))
		require.NoError(t, testutil.CollectAndCompare(o, strings.NewReader(`
# HELP consumer_lag_seconds The consumer lag of each assigned partition, measured as the age of the last record consumed from Kafka.
# TYPE consumer_lag_seconds gauge
consumer_lag_seconds{partition="0"} 5
consumer_lag_seconds{partition="1"} 0
`)))
	})
}
