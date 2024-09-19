package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func BenchmarkGetNextRequest(b *testing.B) {
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
			func(_ int) []string { return nil },
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
				queue := NewRequestQueue(maxOutstandingPerTenant, 0, noQueueLimits, NewMetrics(nil, constants.Loki, "query_scheduler"))
				queues = append(queues, queue)

				for ix := 0; ix < queriers; ix++ {
					queue.RegisterConsumerConnection(fmt.Sprintf("querier-%d", ix))
				}

				for i := 0; i < maxOutstandingPerTenant; i++ {
					for j := 0; j < numTenants; j++ {
						userID := strconv.Itoa(j)
						err := queue.Enqueue(userID, benchCase.fn(j), "request", nil)
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

	for n := 0; n < b.N; n++ {
		q := NewRequestQueue(maxOutstandingPerTenant, 0, noQueueLimits, NewMetrics(nil, constants.Loki, "query_scheduler"))

		for ix := 0; ix < queriers; ix++ {
			q.RegisterConsumerConnection(fmt.Sprintf("querier-%d", ix))
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
				err := queues[n].Enqueue(users[j], nil, requests[j], nil)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

func TestRequestQueue_GetNextRequestForQuerier_ShouldGetRequestAfterReshardingBecauseQuerierHasBeenForgotten(t *testing.T) {
	const forgetDelay = 3 * time.Second

	queue := NewRequestQueue(1, forgetDelay, &mockQueueLimits{maxConsumers: 1}, NewMetrics(nil, constants.Loki, "query_scheduler"))

	// Start the queue service.
	ctx := context.Background()
	require.NoError(t, services.StartAndAwaitRunning(ctx, queue))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(ctx, queue))
	})

	// Two queriers connect.
	queue.RegisterConsumerConnection("querier-1")
	queue.RegisterConsumerConnection("querier-2")

	// Querier-2 waits for a new request.
	querier2wg := sync.WaitGroup{}
	querier2wg.Add(1)
	go func() {
		defer querier2wg.Done()
		_, _, err := queue.Dequeue(ctx, StartIndex, "querier-2")
		require.NoError(t, err)
	}()

	// Querier-1 crashes (no graceful shutdown notification).
	queue.UnregisterConsumerConnection("querier-1")

	// Enqueue a request from an user which would be assigned to querier-1.
	// NOTE: "user-1" hash falls in the querier-1 shard.
	require.NoError(t, queue.Enqueue("user-1", nil, "request", nil))

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
	t.Run("queue size is tracked per tenant", func(t *testing.T) {
		maxSize := 3
		queue := NewRequestQueue(maxSize, 0, noQueueLimits, NewMetrics(nil, constants.Loki, "query_scheduler"))
		queue.RegisterConsumerConnection("querier")

		// enqueue maxSize items with different actors
		// different actors have individual channels with maxSize length
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-a"}, 1, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-b"}, 2, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-c"}, 3, nil))

		// max queue length per tenant is tracked globally for all actors within a tenant
		err := queue.Enqueue("tenant", []string{"user-a"}, 4, nil)
		assert.Equal(t, err, ErrTooManyRequests)

		// dequeue and enqueue some items
		_, _, err = queue.Dequeue(context.Background(), StartIndexWithLocalQueue, "querier")
		assert.NoError(t, err)
		_, _, err = queue.Dequeue(context.Background(), StartIndexWithLocalQueue, "querier")
		assert.NoError(t, err)

		assert.NoError(t, queue.Enqueue("tenant", []string{"user-a"}, 4, nil))
		assert.NoError(t, queue.Enqueue("tenant", []string{"user-b"}, 5, nil))

		err = queue.Enqueue("tenant", []string{"user-c"}, 6, nil)
		assert.Equal(t, err, ErrTooManyRequests)
	})
}

type mockLimits struct {
	maxConsumer int
}

func (l *mockLimits) MaxConsumers(_ string, _ int) int {
	return l.maxConsumer
}

func Test_Queue_DequeueMany(t *testing.T) {
	tenantsQueueMaxSize := 100
	tests := map[string]struct {
		tenantsCount          int
		tasksPerTenant        int
		consumersCount        int
		dequeueBatchSize      int
		maxConsumersPerTenant int
		consumerDelay         time.Duration
		senderDelay           time.Duration
	}{
		"tenants-1_tasks-10_consumers-3_batch-2": {
			tenantsCount:     1,
			tasksPerTenant:   10,
			consumersCount:   3,
			dequeueBatchSize: 2,
		},
		"tenants-10_tasks-10_consumers-2_batch-10": {
			tenantsCount:     10,
			tasksPerTenant:   10,
			consumersCount:   2,
			dequeueBatchSize: 10,
		},
		"tenants-100_tasks-100_consumers-10_batch-10": {
			tenantsCount:     100,
			tasksPerTenant:   100,
			consumersCount:   10,
			dequeueBatchSize: 10,
		},
		"tenants-100_tasks-100_consumers-10_batch-10_consumerDelay-10": {
			tenantsCount:     100,
			tasksPerTenant:   100,
			consumersCount:   10,
			dequeueBatchSize: 10,
			consumerDelay:    10 * time.Millisecond,
		},
		"tenants-100_tasks-100_consumers-10_batch-10_consumerDelay-10_senderDelay-10": {
			tenantsCount:     100,
			tasksPerTenant:   100,
			consumersCount:   10,
			dequeueBatchSize: 10,
			consumerDelay:    10 * time.Millisecond,
			senderDelay:      10 * time.Millisecond,
		},
		"tenants-10_tasks-50_consumers-10_batch-3": {
			tenantsCount:     10,
			tasksPerTenant:   50,
			consumersCount:   10,
			dequeueBatchSize: 3,
		},
		"tenants-10_tasks-50_consumers-1_batch-5": {
			tenantsCount:     10,
			tasksPerTenant:   50,
			consumersCount:   1,
			dequeueBatchSize: 5,
		},
		"tenants-5_tasks-10_consumers-1_batch-2": {
			tenantsCount:     5,
			tasksPerTenant:   10,
			consumersCount:   1,
			dequeueBatchSize: 2,
		},
		"tenants-1_tasks-10_consumers-1_batch-2": {
			tenantsCount:     1,
			tasksPerTenant:   10,
			consumersCount:   1,
			dequeueBatchSize: 2,
		},
		"tenants-1_tasks-10_consumers-10_batch-2": {
			tenantsCount:     1,
			tasksPerTenant:   10,
			consumersCount:   10,
			dequeueBatchSize: 2,
		},
		"tenants-100_tasks-10_consumers-10_batch-6_maxConsumersPerTenant-6": {
			tenantsCount:          100,
			tasksPerTenant:        10,
			consumersCount:        10,
			dequeueBatchSize:      6,
			maxConsumersPerTenant: 4,
		},
		"tenants-200_tasks-10_consumers-100_batch-999999_maxConsumersPerTenant-4": {
			tenantsCount:          200,
			tasksPerTenant:        10,
			consumersCount:        100,
			dequeueBatchSize:      999_999,
			maxConsumersPerTenant: 4,
		},
		"tenants-10_tasks-100_consumers-20_batch-5_maxConsumersPerTenant-16": {
			tenantsCount:          10,
			tasksPerTenant:        100,
			consumersCount:        20,
			dequeueBatchSize:      5,
			maxConsumersPerTenant: 16,
		},
		"tenants-1_tasks-100_consumers-16_batch-5_maxConsumersPerTenant-2": {
			tenantsCount:          1,
			tasksPerTenant:        100,
			consumersCount:        16,
			dequeueBatchSize:      5,
			maxConsumersPerTenant: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			totalTasksCount := tt.tenantsCount * tt.tasksPerTenant
			limits := &mockLimits{maxConsumer: tt.maxConsumersPerTenant}
			require.LessOrEqual(t, tt.tasksPerTenant, tenantsQueueMaxSize, "test must be able to enqueue all the tasks without error")
			queue := NewRequestQueue(tenantsQueueMaxSize, 0, limits, NewMetrics(nil, constants.Loki, "query_scheduler"))
			for i := 0; i < tt.consumersCount; i++ {
				queue.RegisterConsumerConnection(createConsumerName(i))
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wg, _ := errgroup.WithContext(ctx)

			mtx := &sync.Mutex{}
			receivedTasksPerTenant := make(map[string]int, tt.tenantsCount)
			receivedTasksCount := atomic.NewInt32(0)
			for i := 0; i < tt.consumersCount; i++ {
				consumer := createConsumerName(i)
				wg.Go(func() error {
					idx := StartIndexWithLocalQueue
					for {
						tasks, newIdx, err := queue.DequeueMany(ctx, idx, consumer, tt.dequeueBatchSize)

						if err != nil && !errors.Is(err, context.Canceled) {
							return fmt.Errorf("error while dequeueing many task by %s: %w", consumer, err)
						}
						if err == nil && len(tasks) == 0 {
							return fmt.Errorf("%s must receive at least one item if there is no error, but got 0", consumer)
						}
						if len(tasks) > 1 && !isAllItemsSame(tasks) {
							return fmt.Errorf("expected all items to be from the same tenant, but they were not: %v", tasks)
						}

						time.Sleep(tt.consumerDelay)
						collectTasks(mtx, err, tasks, receivedTasksPerTenant)
						receivedTasksTotal := receivedTasksCount.Add(int32(len(tasks)))
						// put the slice back to the pool
						queue.ReleaseRequests(tasks)
						if receivedTasksTotal == int32(totalTasksCount) {
							//cancel the context to release the rest of the consumers once we received all the tasks from all the queues
							cancel()
							return nil
						}
						idx = newIdx
					}
				})
			}

			enqueueTasksAsync(tt.tenantsCount, tt.tasksPerTenant, tt.senderDelay, wg, queue)

			require.NoError(t, wg.Wait())
			require.Len(t, receivedTasksPerTenant, tt.tenantsCount)
			for tenant, actualTasksCount := range receivedTasksPerTenant {
				require.Equalf(t, tt.tasksPerTenant, actualTasksCount, "%s received less tasks than expected", tenant)
			}
		})
	}
}

func enqueueTasksAsync(tenantsCount int, tasksPerTenant int, senderDelay time.Duration, wg *errgroup.Group, queue *RequestQueue) {
	for i := 0; i < tenantsCount; i++ {
		tenant := fmt.Sprintf("tenant-%d", i)
		wg.Go(func() error {
			for j := 0; j < tasksPerTenant; j++ {
				err := queue.Enqueue(tenant, []string{}, tenant, nil)
				if err != nil {
					return fmt.Errorf("error while enqueueing task for the %s : %w", tenant, err)
				}
				time.Sleep(senderDelay)
			}
			return nil
		})
	}
}

func collectTasks(mtx *sync.Mutex, err error, tasks []Request, receivedTasksPerTenant map[string]int) {
	if err != nil {
		return
	}
	mtx.Lock()
	defer mtx.Unlock()
	//tenant name is sent as task
	tenant := tasks[0]
	receivedTasksPerTenant[fmt.Sprintf("%v", tenant)] += len(tasks)
}

func createConsumerName(i int) string {
	return fmt.Sprintf("consumer-%d", i)
}

func isAllItemsSame[T comparable](items []T) bool {
	firstItem := items[0]
	for i := 1; i < len(items); i++ {
		if items[i] != firstItem {
			return false
		}
	}
	return true
}

func assertChanReceived(t *testing.T, c chan struct{}, timeout time.Duration, msg string) {
	t.Helper()

	select {
	case <-c:
	case <-time.After(timeout):
		t.Fatal(msg)
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
