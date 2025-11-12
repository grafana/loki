package ingester

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type dumbFlusher struct {
	onFlush func()
}

func newDumbFlusher(onFlush func()) *dumbFlusher {
	return &dumbFlusher{
		onFlush: onFlush,
	}
}

func (f *dumbFlusher) Flush() {
	if f.onFlush != nil {
		f.onFlush()
	}
}

func nilMetrics() *ingesterMetrics { return newIngesterMetrics(nil, constants.Loki) }

func TestReplayController(t *testing.T) {
	var ops []string
	var opLock sync.Mutex

	var rc *replayController
	flusher := newDumbFlusher(
		func() {
			rc.Sub(100) // simulate flushing 100 bytes
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "Flush")
		},
	)
	rc = newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)

	var wg sync.WaitGroup
	n := 5
	wg.Add(n)

	for i := 0; i < n; i++ {
		// In order to prevent all the goroutines from running before they've added bytes
		// to the internal count, introduce a brief sleep.
		time.Sleep(time.Millisecond)

		// nolint:errcheck,unparam
		go rc.WithBackPressure(func() error {
			rc.Add(50)
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "WithBackPressure")
			wg.Done()
			return nil
		})
	}

	wg.Wait()

	expected := []string{
		"WithBackPressure", // add 50, total 50
		"WithBackPressure", // add 50, total 100
		"Flush",            // subtract 100, total 0
		"WithBackPressure", // add 50, total 50
		"WithBackPressure", // add 50, total 100
		"Flush",            // subtract 100, total 0
		"WithBackPressure", // add 50, total 50
	}
	require.Equal(t, expected, ops)
}

// Test to ensure only one flush happens at a time when multiple goroutines call WithBackPressure
func TestReplayControllerConcurrentFlushes(t *testing.T) {
	t.Run("multiple goroutines wait for single flush", func(t *testing.T) {
		var rc *replayController

		var flushesStarted atomic.Int32
		var flushInProgress atomic.Bool

		flusher := newDumbFlusher(func() {
			// Check if a flush is already in progress, fail if there is one.
			if !flushInProgress.CompareAndSwap(false, true) {
				t.Error("Multiple flushes running concurrently!")
			}

			flushesStarted.Add(1)

			time.Sleep(100 * time.Millisecond) // Simulate a slow flush

			rc.Sub(200)

			flushInProgress.Store(false)
		})

		rc = newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)

		// Fill to trigger flush condition (90% of 100 = 90 bytes threshold)
		rc.Add(95)

		// Launch multiple goroutines simultaneously
		start := make(chan struct{})
		var wg sync.WaitGroup
		numGoroutines := 5

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start

				err := rc.WithBackPressure(func() error {
					rc.Add(20) // Each would trigger flush condition
					return nil
				})
				require.NoError(t, err)
			}()
		}

		// Start all goroutines at the same time
		close(start)

		wg.Wait()

		// All goroutines should have shared a single flush
		require.Equal(t, int32(1), flushesStarted.Load(),
			"Singleflight should coalesce all flush requests into one")
	})
}
