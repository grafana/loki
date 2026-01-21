package distributor

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBasicPushTracker(t *testing.T) {
	t.Run("a new tracker that has never been incremented should never block", func(t *testing.T) {
		tracker := newPushTracker()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.NoError(t, tracker.Wait(ctx))
	})

	t.Run("a canceled context should return a context canceled error", func(t *testing.T) {
		tracker := newPushTracker()
		tracker.Add(1)
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		t.Cleanup(cancel)
		require.EqualError(t, tracker.Wait(ctx), "context deadline exceeded")
	})

	t.Run("a done tracker with no errors should return nil", func(t *testing.T) {
		tracker := newPushTracker()
		tracker.Add(1)
		tracker.Done(nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.NoError(t, tracker.Wait(ctx))
	})

	t.Run("a done tracker with an error should return the error", func(t *testing.T) {
		tracker := newPushTracker()
		tracker.Add(1)
		tracker.Done(errors.New("an error occurred"))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.EqualError(t, tracker.Wait(ctx), "an error occurred")
	})

	t.Run("a done tracker should return the first error that occurred", func(t *testing.T) {
		tracker := newPushTracker()
		tracker.Add(2)
		tracker.Done(errors.New("an error occurred"))
		tracker.Done(errors.New("another error occurred"))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.EqualError(t, tracker.Wait(ctx), "an error occurred")
	})

	t.Run("a done tracker should return at least one error", func(t *testing.T) {
		t1 := newPushTracker()
		t1.Add(2)
		t1.Done(nil)
		t1.Done(errors.New("an error occurred"))
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.EqualError(t, t1.Wait(ctx), "an error occurred")
		// And now test the opposite sequence.
		t2 := newPushTracker()
		t2.Add(2)
		t2.Done(errors.New("an error occurred"))
		t2.Done(nil)
		ctx, cancel = context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		require.EqualError(t, t2.Wait(ctx), "an error occurred")
	})

	t.Run("more Done than Add should panic", func(t *testing.T) {
		// Should panic if Done is called before Add.
		require.PanicsWithValue(t, "Done called more times than Add", func() {
			tracker := newPushTracker()
			tracker.Done(nil)
		})
		// Should panic if Done is called more times than Add.
		require.PanicsWithValue(t, "Done called more times than Add", func() {
			tracker := newPushTracker()
			tracker.Add(1)
			tracker.Done(nil)
			tracker.Done(nil)
		})
	})

	t.Run("Add after Done should panic", func(t *testing.T) {
		require.PanicsWithValue(t, "Add called after last call to Done", func() {
			tracker := newPushTracker()
			tracker.Add(1)
			tracker.Done(nil)
			tracker.Add(1)
		})
	})

	t.Run("Negative counter should panic", func(t *testing.T) {
		require.PanicsWithValue(t, "Negative counter", func() {
			tracker := newPushTracker()
			tracker.Add(-1)
		})
	})
}

// Run with go test -fuzz=FuzzBasicPushTracker.
func FuzzBasicPushTracker(f *testing.F) {
	f.Add(uint16(100))
	f.Fuzz(func(t *testing.T, n uint16) {
		wg := sync.WaitGroup{}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		t.Cleanup(cancel)
		tracker := newPushTracker()
		tracker.Add(int32(n))
		// Create a random number of waiters.
		for i := 0; i < rand.Intn(100); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Sleep a random time up to 100ms.
				time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
				require.NoError(t, tracker.Wait(ctx))
			}()
		}
		// Done should be called for each n, cannot be random.
		for i := 0; i < int(n); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Sleep a random time up to 100ms too.
				time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
				tracker.Done(nil)
			}()
		}
		wg.Wait()
	})
}
