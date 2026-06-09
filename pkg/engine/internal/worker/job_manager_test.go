package worker

import (
	"context"
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
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()
			job := new(threadJob)

			recvDone := make(chan struct{})
			go func() {
				defer close(recvDone)
				actual, err := jm.Recv(t.Context())
				require.NoError(t, err, "Recv should not fail when a job is sent")
				require.Equal(t, job, actual, "Recv should return the sent job")
			}()

			// Wait for the Recv call to be registered as waiting.
			synctest.Wait()

			require.NoError(t, jm.Send(t.Context(), job), "Send should not fail when a Recv call is active")
			<-recvDone
		})
	})

	t.Run("Send fails if the receiver exits", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			recvCtx, cancelRecv := context.WithCancel(t.Context())
			go func() {
				_, _ = jm.Recv(recvCtx)
			}()

			// Wait for the Recv call to be registered as waiting.
			synctest.Wait()

			cancelRecv()
			synctest.Wait()

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

			// Wait for all Recv calls to be waiting.
			synctest.Wait()

			for range 10 {
				require.NoError(t, jm.Send(t.Context(), new(threadJob)), "Send should not fail when there are waiting threads")
			}

			// Test sending beyond how many receivers there were.
			err := jm.Send(t.Context(), new(threadJob))
			require.ErrorIs(t, err, errNoReadyThreads, "Send should fail if there are no ready threads")
		})
	})

	t.Run("WaitReady blocks until a thread becomes ready", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			done := make(chan struct{})
			go func() {
				defer close(done)
				err := jm.WaitReady(t.Context())
				require.NoError(t, err)
			}()

			// No ready threads yet: WaitReady must block.
			synctest.Wait()
			select {
			case <-done:
				t.Fatal("WaitReady returned before any thread became ready")
			default:
			}

			// A thread becoming ready unblocks WaitReady.
			go func() { _, _ = jm.Recv(t.Context()) }()
			synctest.Wait()
			<-done
		})
	})

	t.Run("WaitReady coalesces a burst of newly-ready threads", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			// Several threads become ready before anyone observes the generation.
			for range 5 {
				go func() { _, _ = jm.Recv(t.Context()) }()
			}
			synctest.Wait()

			// A single call observes the whole burst as one generation jump.
			err := jm.WaitReady(t.Context())
			require.NoError(t, err)
		})
	})

	t.Run("WaitReady returns when the context is cancelled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			jm := newJobManager()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			err := jm.WaitReady(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	})
}
