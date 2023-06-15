package queue

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	loki_nats "github.com/grafana/loki/pkg/nats"
)

func BenchmarkGetNextRequest(b *testing.B) {
	logger := log.NewNopLogger()

	const maxOutstandingPerTenant = 2
	const numTenants = 50
	const queriers = 5

	type generateActor func(i int) []string

	benchCases := []struct {
		name string
		fn   generateActor
	}{
		{
			"without sub-queues",
			func(i int) []string { return nil },
		},
		{
			"with 1 level of sub-queues",
			func(i int) []string { return []string{fmt.Sprintf("user-%d", i%11)} },
		},
		{
			"with 2 levels of sub-queues",
			func(i int) []string { return []string{fmt.Sprintf("user-%d", i%11), fmt.Sprintf("tool-%d", i%9)} },
		},
	}

	for _, benchCase := range benchCases {
		benchCase := benchCase

		b.Run(benchCase.name, func(b *testing.B) {

			queues := make([]*RequestQueue, 0, b.N)
			for n := 0; n < b.N; n++ {
				queue, _ := NewRequestQueue(maxOutstandingPerTenant, 0, loki_nats.Config{}, logger, NewMetrics("query_scheduler", nil))
				queues = append(queues, queue)

				for ix := 0; ix < queriers; ix++ {
					queue.RegisterQuerierConnection(fmt.Sprintf("querier-%d", ix))
				}

				for i := 0; i < maxOutstandingPerTenant; i++ {
					for j := 0; j < numTenants; j++ {
						userID := strconv.Itoa(j)
						err := queue.Enqueue(userID, benchCase.fn(j), "request", 0, nil)
						if err != nil {
							b.Fatal(err)
						}
					}
				}

			}

			querierNames := make([]string, queriers)
			for x := 0; x < queriers; x++ {
				querierNames[x] = fmt.Sprintf("querier-%d", x)
			}

			ctx := context.Background()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				for j := 0; j < queriers; j++ {
					idx := StartIndexWithLocalQueue
					for x := 0; x < maxOutstandingPerTenant*numTenants/queriers; x++ {
						r, nidx, err := queues[i].Dequeue(ctx, idx, querierNames[j])
						if r == nil {
							break
						}
						if err != nil {
							b.Fatal(err)
						}
						idx = nidx
					}
				}
			}

		})
	}

}

func BenchmarkQueueRequest(b *testing.B) {
	const maxOutstandingPerTenant = 2
	const numTenants = 50
	const queriers = 5

	queues := make([]*RequestQueue, 0, b.N)
	users := make([]string, 0, numTenants)
	requests := make([]string, 0, numTenants)

	logger := log.NewNopLogger()

	for n := 0; n < b.N; n++ {
		q, _ := NewRequestQueue(maxOutstandingPerTenant, 0, loki_nats.Config{}, logger, NewMetrics("query_scheduler", nil))

		for ix := 0; ix < queriers; ix++ {
			q.RegisterQuerierConnection(fmt.Sprintf("querier-%d", ix))
		}

		queues = append(queues, q)

		for j := 0; j < numTenants; j++ {
			requests = append(requests, fmt.Sprintf("%d-%d", n, j))
			users = append(users, strconv.Itoa(j))
		}
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := 0; i < maxOutstandingPerTenant; i++ {
			for j := 0; j < numTenants; j++ {
				err := queues[n].Enqueue(users[j], nil, requests[j], 0, nil)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

func TestRequestQueue_GetNextRequestForQuerier_ShouldGetRequestAfterReshardingBecauseQuerierHasBeenForgotten(t *testing.T) {
	logger := log.NewNopLogger()
	const forgetDelay = 3 * time.Second

	queue, _ := NewRequestQueue(1, forgetDelay, loki_nats.Config{}, logger, NewMetrics("query_scheduler", nil))

	// Start the queue service.
	ctx := context.Background()
	require.NoError(t, services.StartAndAwaitRunning(ctx, queue))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(ctx, queue))
	})

	// Two queriers connect.
	queue.RegisterQuerierConnection("querier-1")
	queue.RegisterQuerierConnection("querier-2")

	// Querier-2 waits for a new request.
	querier2wg := sync.WaitGroup{}
	querier2wg.Add(1)
	go func() {
		defer querier2wg.Done()
		_, _, err := queue.Dequeue(ctx, StartIndex, "querier-2")
		require.NoError(t, err)
	}()

	// Querier-1 crashes (no graceful shutdown notification).
	queue.UnregisterQuerierConnection("querier-1")

	// Enqueue a request from an user which would be assigned to querier-1.
	// NOTE: "user-1" hash falls in the querier-1 shard.
	require.NoError(t, queue.Enqueue("user-1", nil, "request", 1, nil))

	startTime := time.Now()
	querier2wg.Wait()
	waitTime := time.Since(startTime)

	// We expect that querier-2 got the request only after querier-1 forget delay is passed.
	assert.GreaterOrEqual(t, waitTime.Milliseconds(), forgetDelay.Milliseconds())
}

func TestContextCond(t *testing.T) {
	t.Run("wait until broadcast", func(t *testing.T) {
		t.Parallel()
		mtx := &sync.Mutex{}
		cond := contextCond{Cond: sync.NewCond(mtx)}

		doneWaiting := make(chan struct{})

		mtx.Lock()
		go func() {
			cond.Wait(context.Background())
			mtx.Unlock()
			close(doneWaiting)
		}()

		assertChanNotReceived(t, doneWaiting, 100*time.Millisecond, "cond.Wait returned, but it should not because we did not broadcast yet")

		cond.Broadcast()
		assertChanReceived(t, doneWaiting, 250*time.Millisecond, "cond.Wait did not return after broadcast")
	})

	t.Run("wait until context deadline", func(t *testing.T) {
		t.Parallel()
		mtx := &sync.Mutex{}
		cond := contextCond{Cond: sync.NewCond(mtx)}
		doneWaiting := make(chan struct{})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mtx.Lock()
		go func() {
			cond.Wait(ctx)
			mtx.Unlock()
			close(doneWaiting)
		}()

		assertChanNotReceived(t, doneWaiting, 100*time.Millisecond, "cond.Wait returned, but it should not because we did not broadcast yet and didn't cancel the context")

		cancel()
		assertChanReceived(t, doneWaiting, 250*time.Millisecond, "cond.Wait did not return after cancelling the context")
	})

	t.Run("wait on already canceled context", func(t *testing.T) {
		// This test represents the racy real world scenario,
		// we don't know whether it's going to wait before the broadcast triggered by the context cancellation.
		t.Parallel()
		mtx := &sync.Mutex{}
		cond := contextCond{Cond: sync.NewCond(mtx)}
		doneWaiting := make(chan struct{})

		alreadyCanceledContext, cancel := context.WithCancel(context.Background())
		cancel()

		mtx.Lock()
		go func() {
			cond.Wait(alreadyCanceledContext)
			mtx.Unlock()
			close(doneWaiting)
		}()

		assertChanReceived(t, doneWaiting, 250*time.Millisecond, "cond.Wait did not return after cancelling the context")
	})

	t.Run("wait on already canceled context, but it takes a while to wait", func(t *testing.T) {
		t.Parallel()
		mtx := &sync.Mutex{}
		cond := contextCond{
			Cond: sync.NewCond(mtx),
			testHookBeforeWaiting: func() {
				// This makes the waiting goroutine so slow that out Wait(ctx) will need to broadcast once it sees it waiting.
				time.Sleep(250 * time.Millisecond)
			},
		}
		doneWaiting := make(chan struct{})

		alreadyCanceledContext, cancel := context.WithCancel(context.Background())
		cancel()

		mtx.Lock()
		go func() {
			cond.Wait(alreadyCanceledContext)
			mtx.Unlock()
			close(doneWaiting)
		}()

		assertChanReceived(t, doneWaiting, time.Second, "cond.Wait did not return after 500ms")
	})

	t.Run("lots of goroutines waiting at the same time, none of them misses it's broadcast from cancel", func(t *testing.T) {
		t.Parallel()
		mtx := &sync.Mutex{}
		cond := contextCond{
			Cond: sync.NewCond(mtx),
			testHookBeforeWaiting: func() {
				// Wait just a little bit to create every goroutine
				time.Sleep(time.Millisecond)
			},
		}
		const goroutines = 100

		doneWaiting := make(chan struct{}, goroutines)
		release := make(chan struct{})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		for i := 0; i < goroutines; i++ {
			go func() {
				<-release

				mtx.Lock()
				cond.Wait(ctx)
				mtx.Unlock()

				doneWaiting <- struct{}{}
			}()
		}
		go func() {
			<-release
			cancel()
		}()

		close(release)

		assert.Eventually(t, func() bool {
			return len(doneWaiting) == goroutines
		}, time.Second, 10*time.Millisecond)
	})
}

func TestMaxQueueSize(t *testing.T) {
	logger := log.NewNopLogger()
	t.Run("queue size is tracked per tenant", func(t *testing.T) {
		maxSize := 3
		queue, _ := NewRequestQueue(maxSize, 0, loki_nats.Config{}, logger, NewMetrics("query_scheduler", nil))
		queue.RegisterQuerierConnection("querier")

		// enqueue maxSize items with different actors
		// different actors have individual channels with maxSize length
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-a"}, 1, 0, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-b"}, 2, 0, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-c"}, 3, 0, nil))

		// max queue length per tenant is tracked globally for all actors within a tenant
		err := queue.Enqueue("tenant", []string{"user-a"}, 4, 0, nil)
		assert.Equal(t, err, ErrTooManyRequests)

		// dequeue and enqueue some items
		_, _, err = queue.Dequeue(context.Background(), StartIndexWithLocalQueue, "querier")
		assert.NoError(t, err)
		_, _, err = queue.Dequeue(context.Background(), StartIndexWithLocalQueue, "querier")
		assert.NoError(t, err)

		assert.NoError(t, queue.Enqueue("tenant", []string{"user-a"}, 4, 0, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-b"}, 5, 0, nil))

		err = queue.Enqueue("tenant", []string{"user-c"}, 6, 0, nil)
		assert.Equal(t, err, ErrTooManyRequests)
	})
}

func assertChanReceived(t *testing.T, c chan struct{}, timeout time.Duration, msg string) {
	t.Helper()

	select {
	case <-c:
	case <-time.After(timeout):
		t.Fatalf(msg)
	}
}

func assertChanNotReceived(t *testing.T, c chan struct{}, wait time.Duration, msg string, args ...interface{}) {
	t.Helper()

	select {
	case <-c:
		t.Fatalf(msg, args...)
	case <-time.After(wait):
		// OK!
	}
}
