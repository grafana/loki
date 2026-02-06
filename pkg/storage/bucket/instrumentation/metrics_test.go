package instrumentation

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/cristalhq/hedgedhttp"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestDiffCounter(t *testing.T) {
	ctr := prometheus.NewCounter(prometheus.CounterOpts{Name: fmt.Sprintf("counter_%d", rand.Uint64())})
	dc := &diffCounter{previous: 0, counter: ctr}

	dc.addAbsoluteToCounter(5)
	require.Equal(t, 5.0, ctrVal(t, ctr))

	dc.addAbsoluteToCounter(7)
	require.Equal(t, 7.0, ctrVal(t, ctr))

	dc.addAbsoluteToCounter(57)
	require.Equal(t, 57.0, ctrVal(t, ctr))
}

// MockStatsProvider is StatsProvider for testing
type MockStatsProvider struct {
	mu                  sync.Mutex
	actualRoundTrips    uint64
	requestedRoundTrips uint64
}

func (m *MockStatsProvider) Snapshot() hedgedhttp.StatsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return hedgedhttp.StatsSnapshot{
		ActualRoundTrips:    m.actualRoundTrips,
		RequestedRoundTrips: m.requestedRoundTrips,
	}
}

func (m *MockStatsProvider) SetStats(actual, requested uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actualRoundTrips = actual
	m.requestedRoundTrips = requested
}

func TestPublish(t *testing.T) {
	ctr := prometheus.NewCounter(prometheus.CounterOpts{Name: fmt.Sprintf("counter_%d", rand.Uint64())})
	stats := &MockStatsProvider{}

	publishWithDuration(stats, ctr, 10*time.Millisecond)

	require.Equal(t, 0.0, ctrVal(t, ctr))

	// Set initial stats values
	stats.SetStats(5, 5)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 0.0, ctrVal(t, ctr))

	stats.SetStats(15, 10)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 5.0, ctrVal(t, ctr))

	stats.SetStats(28, 20)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 8.0, ctrVal(t, ctr))

	stats.SetStats(38, 25)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 13.0, ctrVal(t, ctr))

	time.Sleep(30 * time.Millisecond)

	// counter doesn't increase if stats stay same
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 13.0, ctrVal(t, ctr))
}

func ctrVal(t *testing.T, ctr prometheus.Counter) float64 {
	t.Helper()

	m := &dto.Metric{}
	err := ctr.Write(m)

	require.NoError(t, err)

	return m.Counter.GetValue()
}
