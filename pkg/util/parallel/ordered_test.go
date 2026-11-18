package parallel

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestForEachOrdered(t *testing.T) {
	t.Run("yields the results in index order, one at a time, even when early tasks finish last", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const n = 20
			var tasks gauge
			var yields yieldRecorder

			err := ForEachOrdered(t.Context(), n, 4, sleepInReverseOrder(n, &tasks), yields.yield)

			require.NoError(t, err)
			require.Equal(t, indexesUpTo(n), yields.indexes)
			require.Equal(t, int64(1), yields.active.peak.Load(), "yield must not run concurrently")
			require.Equal(t, int64(4), tasks.peak.Load(), "the tasks must overlap up to the parallelism")
		})
	})

	t.Run("runs as many tasks at once as the parallelism allows, and no more", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			tasks := newBlockingTasks(func(int) bool { return true })
			done := goForEachOrdered(t.Context(), 30, 3, tasks.work, ignoreResult)

			synctest.Wait()
			require.Equal(t, int64(3), tasks.running.current.Load(), "3 tasks run, and the others wait for a slot")

			close(tasks.release)
			require.NoError(t, <-done)
			require.Equal(t, int64(3), tasks.running.peak.Load())
			require.Equal(t, int64(30), tasks.started.Load())
		})
	})

	t.Run("holds a slot until yield returns, so a slow first task bounds the started tasks", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			tasks := newBlockingTasks(func(i int) bool { return i == 0 })
			done := goForEachOrdered(t.Context(), 10, 2, tasks.work, ignoreResult)

			synctest.Wait()
			require.Equal(t, int64(2), tasks.started.Load(), "task 1 is done, but task 0 holds its slot until yield")

			close(tasks.release)
			require.NoError(t, <-done)
			require.Equal(t, int64(10), tasks.started.Load())
		})
	})

	t.Run("holds a slot while yield runs, so a slow yield bounds the started tasks", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var started atomic.Int64
			release := make(chan struct{})
			done := goForEachOrdered(t.Context(), 10, 2,
				func(_ context.Context, i int) (int, error) {
					started.Add(1)
					return i, nil
				},
				func(int, int) error {
					<-release
					return nil
				})

			synctest.Wait()
			require.Equal(t, int64(2), started.Load(), "yield of task 0 is slow, and task 0 holds its slot until it returns")

			close(release)
			require.NoError(t, <-done)
			require.Equal(t, int64(10), started.Load())
		})
	})

	t.Run("runs the tasks one at a time when the parallelism is below 1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var tasks gauge

			err := ForEachOrdered(t.Context(), 5, 0, sleepInReverseOrder(5, &tasks), ignoreResult)

			require.NoError(t, err)
			require.Equal(t, int64(1), tasks.peak.Load())
		})
	})

	t.Run("yields every result when there are fewer tasks than the parallelism", func(t *testing.T) {
		var yields yieldRecorder

		err := ForEachOrdered(t.Context(), 2, 8,
			func(_ context.Context, i int) (int, error) { return i, nil },
			yields.yield)

		require.NoError(t, err)
		require.Equal(t, []int{0, 1}, yields.indexes)
	})

	t.Run("returns the error of a task and cancels the other tasks", func(t *testing.T) {
		errBoom := errors.New("boom")

		err := ForEachOrdered(t.Context(), 10, 3,
			func(ctx context.Context, i int) (int, error) {
				if i == 1 {
					return 0, errBoom
				}
				<-ctx.Done()
				return 0, ctx.Err()
			},
			ignoreResult)

		require.ErrorIs(t, err, errBoom)
	})

	t.Run("does not call yield for a result that is ready after another task failed", func(t *testing.T) {
		errBoom := errors.New("boom")
		var yields yieldRecorder

		err := ForEachOrdered(t.Context(), 2, 2,
			func(ctx context.Context, i int) (int, error) {
				if i == 1 {
					return 0, errBoom
				}
				<-ctx.Done()
				return 7, nil
			},
			yields.yield)

		require.ErrorIs(t, err, errBoom)
		require.Empty(t, yields.indexes)
	})

	t.Run("returns the error of yield and stops the tasks", func(t *testing.T) {
		errBoom := errors.New("boom")
		var started atomic.Int64

		err := ForEachOrdered(t.Context(), 100, 2,
			func(context.Context, int) (int, error) {
				started.Add(1)
				return 0, nil
			},
			func(i, _ int) error {
				if i == 2 {
					return errBoom
				}
				return nil
			})

		require.ErrorIs(t, err, errBoom)
		require.Less(t, started.Load(), int64(100))
	})

	t.Run("returns the context error when the context is canceled before the start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := ForEachOrdered(ctx, 5, 2,
			func(ctx context.Context, _ int) (int, error) { return 0, ctx.Err() },
			ignoreResult)

		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("starts no task when the context is canceled before the start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var started atomic.Int64

		err := ForEachOrdered(ctx, 1000, 1000,
			func(context.Context, int) (int, error) {
				started.Add(1)
				return 0, nil
			},
			ignoreResult)

		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, started.Load())
	})

	t.Run("returns the context error and yields nothing when the context is canceled while the tasks run", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			var started atomic.Int64
			var yields yieldRecorder
			done := goForEachOrdered(ctx, 10, 3,
				func(ctx context.Context, _ int) (int, error) {
					started.Add(1)
					<-ctx.Done()
					return 0, ctx.Err()
				},
				yields.yield)

			synctest.Wait()
			require.Equal(t, int64(3), started.Load())

			cancel()
			require.ErrorIs(t, <-done, context.Canceled)
			require.Empty(t, yields.indexes)
			require.Equal(t, int64(3), started.Load(), "the tasks that did not start must stay skipped")
		})
	})

	t.Run("does nothing for zero tasks", func(t *testing.T) {
		err := ForEachOrdered(t.Context(), 0, 4,
			func(context.Context, int) (int, error) { t.Fatal("work must not run"); return 0, nil },
			func(int, int) error { t.Fatal("yield must not run"); return nil })

		require.NoError(t, err)
	})
}

// gauge counts the goroutines that are between enter and leave, and keeps the
// highest count that it saw.
type gauge struct {
	current, peak atomic.Int64
}

func (g *gauge) enter() {
	now := g.current.Add(1)
	for {
		old := g.peak.Load()
		if now <= old || g.peak.CompareAndSwap(old, now) {
			return
		}
	}
}

func (g *gauge) leave() { g.current.Add(-1) }

// yieldRecorder is a yield function that records the indexes that it gets in
// order, and gauges how many calls run at the same time.
type yieldRecorder struct {
	indexes []int
	active  gauge
}

func (r *yieldRecorder) yield(i, _ int) error {
	r.active.enter()
	defer r.active.leave()
	r.indexes = append(r.indexes, i)
	return nil
}

func ignoreResult(int, int) error { return nil }

// blockingTasks is a work function whose tasks can wait until release is
// closed. blocks says which tasks wait. started counts the tasks that began, and
// running gauges the tasks that are inside the function.
type blockingTasks struct {
	release chan struct{}
	blocks  func(i int) bool
	started atomic.Int64
	running gauge
}

func newBlockingTasks(blocks func(i int) bool) *blockingTasks {
	return &blockingTasks{release: make(chan struct{}), blocks: blocks}
}

func (b *blockingTasks) work(_ context.Context, i int) (int, error) {
	b.started.Add(1)
	b.running.enter()
	defer b.running.leave()
	if b.blocks(i) {
		<-b.release
	}
	return i, nil
}

// sleepInReverseOrder returns a work function in which task i sleeps for n-i
// milliseconds, so that the early tasks finish last. tasks gauges the tasks that
// run at the same time.
func sleepInReverseOrder(n int, tasks *gauge) func(context.Context, int) (int, error) {
	return func(_ context.Context, i int) (int, error) {
		tasks.enter()
		defer tasks.leave()
		time.Sleep(time.Duration(n-i) * time.Millisecond)
		return i, nil
	}
}

// goForEachOrdered runs ForEachOrdered on another goroutine. The returned
// channel gets its error.
func goForEachOrdered(ctx context.Context, n, parallelism int, work func(context.Context, int) (int, error), yield func(int, int) error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- ForEachOrdered(ctx, n, parallelism, work, yield) }()
	return done
}

func indexesUpTo(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
