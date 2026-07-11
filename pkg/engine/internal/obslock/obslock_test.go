package obslock

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// TestRWMutexObservesWaitAndHold is a contract test: acquiring and releasing the
// observed mutex must record exactly one wait and one hold sample under the
// lock, mode, and reason of the acquisition.
func TestRWMutexObservesWaitAndHold(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg, "test_lock_wait_seconds", "test_lock_hold_seconds")

	var mut RWMutex
	mut.Init("resourcesMut", metrics)

	writeGuard := mut.Lock("cancel")
	time.Sleep(time.Millisecond)
	writeGuard.Unlock()

	readGuard := mut.RLock("find_tasks")
	time.Sleep(time.Millisecond)
	readGuard.RUnlock()

	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_wait_seconds", map[string]string{
		"lock": "resourcesMut", "mode": modeWrite, "reason": "cancel",
	}))
	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_hold_seconds", map[string]string{
		"lock": "resourcesMut", "mode": modeWrite, "reason": "cancel",
	}))
	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_wait_seconds", map[string]string{
		"lock": "resourcesMut", "mode": modeRead, "reason": "find_tasks",
	}))
	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_hold_seconds", map[string]string{
		"lock": "resourcesMut", "mode": modeRead, "reason": "find_tasks",
	}))
}

// TestMutexObservesWaitAndHold is the exclusive-only counterpart contract test:
// acquiring and releasing the plain observed mutex must record exactly one wait
// and one hold sample, always under mode "write".
func TestMutexObservesWaitAndHold(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg, "test_lock_wait_seconds", "test_lock_hold_seconds")

	var mut Mutex
	mut.Init("conn_write", metrics)

	guard := mut.Lock("send_frame")
	time.Sleep(time.Millisecond)
	guard.Unlock()

	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_wait_seconds", map[string]string{
		"lock": "conn_write", "mode": modeWrite, "reason": "send_frame",
	}))
	require.Equal(t, uint64(1), histogramSampleCount(t, reg, "test_lock_hold_seconds", map[string]string{
		"lock": "conn_write", "mode": modeWrite, "reason": "send_frame",
	}))
}

func histogramSampleCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsEqual(metric, labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}

	require.Failf(t, "metric not found", "metric %s with labels %v was not collected", name, labels)
	return 0
}

func labelsEqual(metric *dto.Metric, want map[string]string) bool {
	if len(metric.GetLabel()) != len(want) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}
