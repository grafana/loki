package util

import (
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestActiveUser(t *testing.T) {
	as := NewActiveUsers()
	as.UpdateUserTimestamp("test1", 5)
	as.UpdateUserTimestamp("test2", 10)
	as.UpdateUserTimestamp("test3", 15)

	require.Nil(t, as.PurgeInactiveUsers(2))
	require.Equal(t, []string{"test1"}, as.PurgeInactiveUsers(5))
	require.Nil(t, as.PurgeInactiveUsers(7))
	require.Equal(t, []string{"test2"}, as.PurgeInactiveUsers(12))

	as.UpdateUserTimestamp("test1", 17)
	require.Equal(t, []string{"test3"}, as.PurgeInactiveUsers(16))
	require.Equal(t, []string{"test1"}, as.PurgeInactiveUsers(20))
}

func TestActiveUserConcurrentUpdateAndPurge(t *testing.T) {
	count := 10

	as := NewActiveUsers()

	done := sync.WaitGroup{}
	stop := atomic.NewBool(false)

	latestTS := atomic.NewInt64(0)

	for j := 0; j < count; j++ {
		done.Add(1)

		go func() {
			defer done.Done()

			for !stop.Load() {
				ts := latestTS.Inc()

				// In each cycle, we update different user.
				as.UpdateUserTimestamp(fmt.Sprintf("%d", ts), ts)

				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	previousLatest := int64(0)
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)

		latest := latestTS.Load()
		require.True(t, latest > previousLatest)

		previousLatest = latest

		purged := as.PurgeInactiveUsers(latest)
		require.NotEmpty(t, purged)
	}

	stop.Store(true)
	done.Wait()

	// Final purge.
	latest := latestTS.Load()
	as.PurgeInactiveUsers(latest)

	// Purging again doesn't do anything.
	purged := as.PurgeInactiveUsers(latest)
	require.Empty(t, purged)
}

func BenchmarkActiveUsers_UpdateUserTimestamp(b *testing.B) {
	for _, c := range []int{0, 5, 10, 25, 50, 100} {
		b.Run(strconv.Itoa(c), func(b *testing.B) {
			as := NewActiveUsers()

			startGoroutinesDoingUpdates(b, c, as)

			for i := 0; i < b.N; i++ {
				as.UpdateUserTimestamp("test", int64(i))
			}
		})
	}
}

func BenchmarkActiveUsers_Purge(b *testing.B) {
	for _, c := range []int{0, 5, 10, 25, 50, 100} {
		b.Run(strconv.Itoa(c), func(b *testing.B) {
			as := NewActiveUsers()

			startGoroutinesDoingUpdates(b, c, as)

			for i := 0; i < b.N; i++ {
				as.PurgeInactiveUsers(int64(i))
			}
		})
	}
}

func startGoroutinesDoingUpdates(b *testing.B, count int, as *ActiveUsers) {
	done := sync.WaitGroup{}
	stop := atomic.NewBool(false)

	started := sync.WaitGroup{}
	for j := 0; j < count; j++ {
		done.Add(1)
		started.Add(1)
		userID := fmt.Sprintf("user-%d", j)
		go func() {
			defer done.Done()
			started.Done()

			ts := int64(0)
			for !stop.Load() {
				ts++
				as.UpdateUserTimestamp(userID, ts)

				// Give other goroutines a chance too.
				if ts%1000 == 0 {
					runtime.Gosched()
				}
			}
		}()
	}
	started.Wait()

	b.Cleanup(func() {
		// Ask goroutines to stop, and then wait until they do.
		stop.Store(true)
		done.Wait()
	})
}
