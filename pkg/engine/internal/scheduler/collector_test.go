package scheduler

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/obslock"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

func TestCollector_Collect(t *testing.T) {
	t.Run("releases scheduler locks while it waits to send a metric", func(t *testing.T) {
		sched, err := New(Config{
			Logger:   log.NewNopLogger(),
			Listener: &wire.Local{Address: wire.LocalScheduler},
		})
		require.NoError(t, err)

		// An unbuffered channel that nobody reads stands in for a registry
		// that is busy evaluating earlier metrics.
		metrics := make(chan prometheus.Metric)
		go func() {
			sched.collector.Collect(metrics)
			close(metrics)
		}()
		defer func() {
			for range metrics {
			}
		}()

		for {
			requireWriteLockable(t, &sched.resourcesMut)
			requireWriteLockable(t, &sched.assignMut)

			if _, ok := <-metrics; !ok {
				return
			}
		}
	})
}

// requireWriteLockable fails the test if mut cannot be write-locked within a
// timeout.
func requireWriteLockable(t *testing.T, mut *obslock.RWMutex) {
	t.Helper()

	locked := make(chan struct{})
	go func() {
		guard := mut.Lock("test")
		guard.Unlock()
		close(locked)
	}()

	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("collector holds a scheduler lock while it waits to send a metric")
	}
}
