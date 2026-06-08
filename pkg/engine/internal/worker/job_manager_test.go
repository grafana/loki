package worker

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_jobManager(t *testing.T) {
	t.Run("WaitReady blocks until a Recv call is waiting", func(t *testing.T) {
		// Use synctest so this doesn't take a real minute to execute.
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			err := jm.WaitReady(ctx)
			require.ErrorIs(t, err, context.DeadlineExceeded, "WaitReady should wait until context cancellation if there are no ready threads")
		})
	})

	t.Run("WaitReady returns when a Recv call is waiting", func(t *testing.T) {
		jm := newJobManager()

		go func() {
			_, _ = jm.Recv(t.Context())
		}()

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		require.NoError(t, jm.WaitReady(ctx), "WaitReady should return when a Recv call is waiting")
	})

	t.Run("Send fails with no calls to Recv", func(t *testing.T) {
		jm := newJobManager()

		err := jm.Send(t.Context(), new(threadJob))
		require.ErrorIs(t, err, errNoReadyThreads, "Send should fail if there are no ready threads")
	})

	t.Run("Send succeeds with a call to Recv", func(t *testing.T) {
		jm := newJobManager()
		job := new(threadJob)

		var wg sync.WaitGroup
		defer wg.Wait()

		wg.Go(func() {
			actual, err := jm.Recv(t.Context())
			require.NoError(t, err, "Recv should not fail when a job is sent")
			require.Equal(t, job, actual, "Recv should return the sent job")
		})

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		require.NoError(t, jm.WaitReady(ctx), "WaitReady should not time out")
		require.NoError(t, jm.Send(t.Context(), job), "Send should not fail when a Recv call is active")
	})

	t.Run("Send fails if the receiver exits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			go func() {
				_, _ = jm.Recv(ctx)
			}()

			require.NoError(t, jm.WaitReady(ctx), "WaitReady should not time out")

			cancel()

			err := jm.Send(t.Context(), new(threadJob))
			require.ErrorIs(t, err, errNoReadyThreads, "Send should fail if there are no ready threads")
		})
	})

	t.Run("Send can be called multiple times", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			for range 10 {
				go func() {
					_, _ = jm.Recv(t.Context())
				}()
			}

			// Wait for all Recv calls to be waiting
			synctest.Wait()
			require.NoError(t, jm.WaitReady(t.Context()), "WaitReady should not fail when a Recv call is waiting")

			for range 10 {
				require.NoError(t, jm.Send(t.Context(), new(threadJob)), "Send should not fail when there are waiting threads")
			}

			// Test sending beyond how many receivers there were.
			err := jm.Send(t.Context(), new(threadJob))
			require.ErrorIs(t, err, errNoReadyThreads, "Send should fail if there are no ready threads")
		})
	})

	t.Run("WaitBusy returns immediately when no thread is waiting", func(t *testing.T) {
		jm := newJobManager()
		require.NoError(t, jm.WaitFull(t.Context()), "WaitBusy should return immediately when the pool is fully busy")
	})

	t.Run("WaitBusy blocks until the waiting thread receives a job", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			go func() {
				_, _ = jm.Recv(t.Context())
			}()

			// Wait for the Recv call to be waiting.
			synctest.Wait()

			busyDone := make(chan error, 1)
			go func() { busyDone <- jm.WaitFull(t.Context()) }()

			// WaitBusy must not return while a thread is still waiting.
			synctest.Wait()
			select {
			case <-busyDone:
				t.Fatal("WaitBusy returned while a thread was still waiting")
			default:
			}

			// Delivering a job makes the waiting thread busy.
			require.NoError(t, jm.Send(t.Context(), new(threadJob)))

			synctest.Wait()
			require.NoError(t, <-busyDone, "WaitBusy should return once every thread is busy")
		})
	})

	t.Run("WaitBusy unblocks when a waiting Recv is cancelled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			recvCtx, cancelRecv := context.WithCancel(t.Context())
			go func() {
				_, _ = jm.Recv(recvCtx)
			}()

			synctest.Wait()

			busyDone := make(chan error, 1)
			go func() { busyDone <- jm.WaitFull(t.Context()) }()

			synctest.Wait()
			select {
			case <-busyDone:
				t.Fatal("WaitBusy returned while a thread was still waiting")
			default:
			}

			// Cancelling the Recv drops the waiting count back to zero.
			cancelRecv()

			synctest.Wait()
			require.NoError(t, <-busyDone, "WaitBusy should return after the waiting Recv is cancelled")
		})
	})

	t.Run("WaitReady/WaitBusy alternate across a busy->ready cycle", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			// Initially fully busy: WaitReady blocks, WaitBusy returns.
			require.NoError(t, jm.WaitFull(t.Context()))

			go func() {
				_, _ = jm.Recv(t.Context())
			}()
			synctest.Wait()

			// A thread is now waiting: WaitReady returns, WaitBusy blocks.
			require.NoError(t, jm.WaitReady(t.Context()), "WaitReady should return once a thread is waiting")

			busyDone := make(chan error, 1)
			go func() { busyDone <- jm.WaitFull(t.Context()) }()
			synctest.Wait()
			select {
			case <-busyDone:
				t.Fatal("WaitBusy returned while a thread was still waiting")
			default:
			}

			// Assign the task: pool is fully busy again, WaitBusy returns.
			require.NoError(t, jm.Send(t.Context(), new(threadJob)))
			synctest.Wait()
			require.NoError(t, <-busyDone, "WaitBusy should return after the thread becomes busy")
		})
	})
}
