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

// Test that WithBackPressure skips backpressure entirely when ReplayMemoryCeiling is zero.
func TestReplayControllerZeroCeiling(t *testing.T) {
	var flushed bool
	flusher := newDumbFlusher(func() {
		flushed = true
	})
	rc := newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 0}, flusher)

	// Add a large amount of data — with a zero ceiling this should not trigger flushing.
	rc.Add(1 << 30) // 1GB

	err := rc.WithBackPressure(func() error {
		rc.Add(1 << 30)
		return nil
	})

	require.NoError(t, err)
	require.False(t, flushed, "flusher should not be called when ReplayMemoryCeiling is zero")
}

// Test that WithBackPressure returns an error when a flush makes no progress reducing memory.
func TestReplayControllerNoProgressFlush(t *testing.T) {
	flusher := newDumbFlusher(nil) // no-op: never calls rc.Sub, so bytes never decrease
	rc := newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)

	// Exceed 90% threshold (ceiling = 90).
	rc.Add(95)

	err := rc.WithBackPressure(func() error {
		return nil
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "WAL replay flush made no progress")
}

// Test that WithBackPressure returns an error when flush makes partial progress but then stalls,
// i.e. subsequent flushes no longer reduce memory below the ceiling.
func TestReplayControllerPartialProgressFlush(t *testing.T) {
	var rc *replayController
	flushCount := 0
	flusher := newDumbFlusher(func() {
		flushCount++
		if flushCount == 1 {
			// First flush makes partial progress: drops from 200 to 95, still above 90% ceiling.
			rc.Sub(105)
		}
		// Subsequent flushes are no-ops: bytes stay at 95, no further progress.
	})
	rc = newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)

	rc.Add(200) // well above the 90-byte ceiling

	err := rc.WithBackPressure(func() error {
		return nil
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "WAL replay flush made no progress")
	require.Equal(t, 2, flushCount, "should have flushed twice: once with progress, once stalled")
}

// Test that WithBackPressure does not spuriously error when concurrent workers refill currentBytes
// during a flush. The flush made real progress (Sub was called), so no error should be returned
// even though currentBytes ends up higher than the pre-flush snapshot due to concurrent Add calls.
//
// Sequence:
//  1. bytes=95 > ceiling=90, enter loop; old code snapshots before=95
//  2. Flush runs: Sub(80) → bytes=15; concurrent goroutine adds 85 → bytes=100
//  3. Old check: currentBytes(100) >= before(95) → spurious error
//  4. New check: totalSubtracted increased → no error, loop continues and drains on next flush
func TestReplayControllerNoSpuriousErrorOnConcurrentAdd(t *testing.T) {
	var rc *replayController
	var once sync.Once
	flushed := make(chan struct{})
	flusher := newDumbFlusher(func() {
		rc.Sub(80)                         // genuine progress each flush
		once.Do(func() { close(flushed) }) // signal the concurrent goroutine only on first flush
	})
	rc = newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)
	rc.Add(95) // above 90% ceiling

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Simulate a concurrent worker adding bytes while the first flush is running,
		// pushing currentBytes back above the pre-flush snapshot (100 > before=95).
		// The loop will flush again and drain normally; no error should be returned.
		<-flushed
		rc.Add(85) // 15 + 85 = 100, above the original before=95
	}()

	err := rc.WithBackPressure(func() error { return nil })
	wg.Wait()

	require.NoError(t, err, "concurrent Add during flush should not cause a spurious no-progress error")
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
