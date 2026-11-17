package parallel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForEachOrdered(t *testing.T) {
	t.Run("yields the results in index order, one at a time, even when early tasks finish last", func(t *testing.T) {
		const n = 20
		var (
			got               []int
			inYield, overlaps atomic.Int64
			running, peak     atomic.Int64
		)
		err := ForEachOrdered(t.Context(), n, 4,
			func(_ context.Context, i int) (int, error) {
				now := running.Add(1)
				for {
					old := peak.Load()
					if now <= old || peak.CompareAndSwap(old, now) {
						break
					}
				}
				defer running.Add(-1)
				time.Sleep(time.Duration(n-i) * time.Millisecond)
				return i * 10, nil
			},
			func(i, result int) error {
				if inYield.Add(1) > 1 {
					overlaps.Add(1)
				}
				defer inYield.Add(-1)
				require.Equal(t, i*10, result)
				got = append(got, i)
				return nil
			})
		require.NoError(t, err)

		want := make([]int, n)
		for i := range want {
			want[i] = i
		}
		require.Equal(t, want, got)
		require.Zero(t, overlaps.Load(), "yield must not run concurrently")
		require.Greater(t, peak.Load(), int64(1), "the tasks must overlap")
	})

	t.Run("runs as many tasks at once as the parallelism allows, and no more", func(t *testing.T) {
		var running, peak atomic.Int64
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- ForEachOrdered(t.Context(), 30, 3,
				func(context.Context, int) (struct{}, error) {
					now := running.Add(1)
					for {
						old := peak.Load()
						if now <= old || peak.CompareAndSwap(old, now) {
							break
						}
					}
					<-release
					running.Add(-1)
					return struct{}{}, nil
				},
				func(int, struct{}) error { return nil })
		}()

		require.Eventually(t, func() bool { return running.Load() == 3 }, time.Second, time.Millisecond)
		require.Never(t, func() bool { return running.Load() > 3 }, 20*time.Millisecond, time.Millisecond)

		close(release)
		require.NoError(t, <-done)
		require.Equal(t, int64(3), peak.Load())
	})

	t.Run("holds a slot until yield returns, so a slow first task bounds the started tasks", func(t *testing.T) {
		var started atomic.Int64
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- ForEachOrdered(t.Context(), 10, 2,
				func(_ context.Context, i int) (int, error) {
					started.Add(1)
					if i == 0 {
						<-release
					}
					return i, nil
				},
				func(int, int) error { return nil })
		}()

		require.Eventually(t, func() bool { return started.Load() == 2 }, time.Second, time.Millisecond)
		require.Never(t, func() bool { return started.Load() > 2 }, 20*time.Millisecond, time.Millisecond,
			"task 1 finished, but task 0 holds its slot until yield")

		close(release)
		require.NoError(t, <-done)
		require.Equal(t, int64(10), started.Load())
	})

	t.Run("runs the tasks one at a time when the parallelism is below 1", func(t *testing.T) {
		var running atomic.Int64
		var overlapped atomic.Bool
		err := ForEachOrdered(t.Context(), 5, 0,
			func(context.Context, int) (struct{}, error) {
				if running.Add(1) > 1 {
					overlapped.Store(true)
				}
				time.Sleep(time.Millisecond)
				running.Add(-1)
				return struct{}{}, nil
			},
			func(int, struct{}) error { return nil })
		require.NoError(t, err)
		require.False(t, overlapped.Load())
	})

	t.Run("yields every result when there are fewer tasks than the parallelism", func(t *testing.T) {
		var got []int
		err := ForEachOrdered(t.Context(), 2, 8,
			func(_ context.Context, i int) (int, error) { return i, nil },
			func(_, result int) error {
				got = append(got, result)
				return nil
			})
		require.NoError(t, err)
		require.Equal(t, []int{0, 1}, got)
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
			func(int, int) error { return nil })
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("does not call yield for a result that is ready after another task failed", func(t *testing.T) {
		errBoom := errors.New("boom")
		var yields atomic.Int64
		err := ForEachOrdered(t.Context(), 2, 2,
			func(ctx context.Context, i int) (int, error) {
				if i == 1 {
					return 0, errBoom
				}
				<-ctx.Done()
				return 7, nil
			},
			func(int, int) error {
				yields.Add(1)
				return nil
			})
		require.ErrorIs(t, err, errBoom)
		require.Zero(t, yields.Load())
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
			func(int, int) error { return nil })
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("returns the context error and yields nothing when the context is canceled while the tasks run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var running, yields atomic.Int64
		done := make(chan error, 1)
		go func() {
			done <- ForEachOrdered(ctx, 10, 3,
				func(ctx context.Context, _ int) (int, error) {
					running.Add(1)
					<-ctx.Done()
					return 0, ctx.Err()
				},
				func(int, int) error {
					yields.Add(1)
					return nil
				})
		}()

		require.Eventually(t, func() bool { return running.Load() == 3 }, time.Second, time.Millisecond)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Zero(t, yields.Load())
		require.Equal(t, int64(3), running.Load(), "the tasks that did not start must stay skipped")
	})

	t.Run("does nothing for zero tasks", func(t *testing.T) {
		err := ForEachOrdered(t.Context(), 0, 4,
			func(context.Context, int) (int, error) { t.Fatal("work must not run"); return 0, nil },
			func(int, int) error { t.Fatal("yield must not run"); return nil })
		require.NoError(t, err)
	})
}
