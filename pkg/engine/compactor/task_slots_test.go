package compactor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var slotsTestWindow = time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

func logMergePriority(level int) taskPriority {
	return taskPriority{class: priorityLogMerge, level: level, window: slotsTestWindow}
}

// waitForSlot starts a wait for a slot of priority p and returns a channel
// that receives the release of the slot once the wait gets one.
func waitForSlot(t *testing.T, slots *taskSlots, p taskPriority) <-chan func() {
	t.Helper()
	granted := make(chan func(), 1)
	go func() {
		release, err := slots.acquire(t.Context(), p)
		if err == nil {
			granted <- release
		}
	}()
	return granted
}

// requireQueuedWaits blocks until slots holds n waits.
func requireQueuedWaits(t *testing.T, slots *taskSlots, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		slots.slotsMu.Lock()
		defer slots.slotsMu.Unlock()
		return len(slots.waits) == n
	}, time.Second, time.Millisecond)
}

// requireFreeSlots checks that slots has n free slots and no waits.
func requireFreeSlots(t *testing.T, slots *taskSlots, n int) {
	t.Helper()
	slots.slotsMu.Lock()
	defer slots.slotsMu.Unlock()
	require.Equal(t, n, slots.free)
	require.Zero(t, len(slots.waits))
}

func TestTaskSlots(t *testing.T) {
	t.Run("grants slots up to the limit at once", func(t *testing.T) {
		slots := newTaskSlots(2)
		_, err := slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)
		_, err = slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		_, err = slots.acquire(ctx, logMergePriority(1))
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("gives a released slot to the wait with the highest priority", func(t *testing.T) {
		slots := newTaskSlots(1)
		release, err := slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)

		level3 := waitForSlot(t, slots, logMergePriority(3))
		requireQueuedWaits(t, slots, 1)
		level2 := waitForSlot(t, slots, logMergePriority(2))
		requireQueuedWaits(t, slots, 2)
		indexMerge := waitForSlot(t, slots, taskPriority{class: priorityIndexMerge, level: 5, window: slotsTestWindow})
		requireQueuedWaits(t, slots, 3)

		release()
		releaseIndexMerge := <-indexMerge
		require.Empty(t, level2)
		releaseIndexMerge()
		releaseLevel2 := <-level2
		require.Empty(t, level3)
		releaseLevel2()
		(<-level3)()
		requireFreeSlots(t, slots, 1)
	})

	t.Run("gives slots to waits of equal priority in arrival order", func(t *testing.T) {
		slots := newTaskSlots(1)
		release, err := slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)

		first := waitForSlot(t, slots, logMergePriority(1))
		requireQueuedWaits(t, slots, 1)
		second := waitForSlot(t, slots, logMergePriority(1))
		requireQueuedWaits(t, slots, 2)

		release()
		releaseFirst := <-first
		require.Empty(t, second)
		releaseFirst()
		(<-second)()
	})

	t.Run("returns the slot of a cancelled wait to the pool", func(t *testing.T) {
		slots := newTaskSlots(1)
		release, err := slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			_, err := slots.acquire(ctx, logMergePriority(1))
			done <- err
		}()
		requireQueuedWaits(t, slots, 1)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		requireQueuedWaits(t, slots, 0)

		release()
		requireFreeSlots(t, slots, 1)
	})

	t.Run("leaks no slot when waits are cancelled while slots are released", func(t *testing.T) {
		const limit = 2
		slots := newTaskSlots(limit)
		for range 500 {
			first, err := slots.acquire(t.Context(), logMergePriority(1))
			require.NoError(t, err)
			second, err := slots.acquire(t.Context(), logMergePriority(1))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			var waits sync.WaitGroup
			for range 4 {
				waits.Go(func() {
					if release, err := slots.acquire(ctx, logMergePriority(1)); err == nil {
						release()
					}
				})
			}
			waits.Go(cancel)
			waits.Go(first)
			waits.Go(second)
			waits.Wait()
		}
		requireFreeSlots(t, slots, limit)
	})

	t.Run("a second call of release frees no extra slot", func(t *testing.T) {
		slots := newTaskSlots(1)
		release, err := slots.acquire(t.Context(), logMergePriority(1))
		require.NoError(t, err)
		release()
		release()
		requireFreeSlots(t, slots, 1)
	})

	t.Run("admits every wait at once without a limit", func(t *testing.T) {
		slots := newTaskSlots(0)
		for range 100 {
			_, err := slots.acquire(t.Context(), logMergePriority(1))
			require.NoError(t, err)
		}
	})

	t.Run("startJob returns an error and holds no slot when ctx is done", func(t *testing.T) {
		slots := newTaskSlots(1)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := slots.startJob(ctx, logMergePriority(1))
		require.ErrorIs(t, err, context.Canceled)
		requireFreeSlots(t, slots, 1)
	})
}

func TestTaskPriorityBefore(t *testing.T) {
	tests := []struct {
		name          string
		first, second taskPriority
	}{
		{
			name:   "a task of a started job runs before an index merge that did not start",
			first:  taskPriority{started: true, class: priorityLogMerge, level: 3},
			second: taskPriority{class: priorityIndexMerge},
		},
		{
			name:   "a started log merge runs before a pair split that did not start",
			first:  taskPriority{started: true, class: priorityLogMerge},
			second: taskPriority{class: priorityPairSplit},
		},
		{
			name:   "an index merge runs before a log merge of a lower level",
			first:  taskPriority{started: true, class: priorityIndexMerge, level: 3},
			second: taskPriority{started: true, class: priorityLogMerge, level: 1},
		},
		{
			name:   "a lower level runs first within one class",
			first:  logMergePriority(1),
			second: logMergePriority(2),
		},
		{
			name:   "an older window runs first within one level",
			first:  logMergePriority(1),
			second: taskPriority{class: priorityLogMerge, level: 1, window: slotsTestWindow.Add(time.Hour)},
		},
		{
			name:   "an earlier planned job runs first within one window",
			first:  taskPriority{class: priorityLogMerge, level: 1, window: slotsTestWindow, planOrder: 1},
			second: taskPriority{class: priorityLogMerge, level: 1, window: slotsTestWindow, planOrder: 2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.True(t, test.first.before(test.second))
			require.False(t, test.second.before(test.first))
		})
	}

	t.Run("equal priorities run in neither order", func(t *testing.T) {
		require.False(t, logMergePriority(1).before(logMergePriority(1)))
	})
}

func TestJobSlots(t *testing.T) {
	t.Run("the first task takes the held slot and later tasks wait for free slots", func(t *testing.T) {
		slots := newTaskSlots(1)
		job, err := slots.startJob(t.Context(), logMergePriority(1))
		require.NoError(t, err)
		admit := job.admit(priorityLogMerge)

		releaseFirst, err := admit(t.Context())
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		_, err = admit(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		releaseFirst()
		releaseSecond, err := admit(t.Context())
		require.NoError(t, err)
		releaseSecond()
		requireFreeSlots(t, slots, 1)
	})

	t.Run("a task of a started job gets a released slot before a job that did not start", func(t *testing.T) {
		slots := newTaskSlots(1)
		job, err := slots.startJob(t.Context(), logMergePriority(3))
		require.NoError(t, err)
		admit := job.admit(priorityLogMerge)
		releaseFirst, err := admit(t.Context())
		require.NoError(t, err)

		unstarted := waitForSlot(t, slots, taskPriority{class: priorityIndexMerge})
		requireQueuedWaits(t, slots, 1)
		second := make(chan func(), 1)
		go func() {
			if release, err := admit(t.Context()); err == nil {
				second <- release
			}
		}()
		requireQueuedWaits(t, slots, 2)

		releaseFirst()
		releaseSecond := <-second
		require.Empty(t, unstarted)
		releaseSecond()
		(<-unstarted)()
	})

	t.Run("the held slot returns to the pool when no task took it", func(t *testing.T) {
		slots := newTaskSlots(1)
		job, err := slots.startJob(t.Context(), logMergePriority(1))
		require.NoError(t, err)

		job.releaseHeld()
		requireFreeSlots(t, slots, 1)
	})
}
