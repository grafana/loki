package queue

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

const (
	numRequestsPerActor = 1
	numQueriers         = 5
)

type req struct {
	duration   time.Duration
	tenant     string
	actor      string
	queryID    int
	subQueryID int
}

func enqueueRequestsForActor(t *testing.T, actor []string, useActor bool, queue *RequestQueue, numSubRequests int, d time.Duration) {
	tenant := "tenant"
	serializedActor := strings.Join(actor, "|")
	for x := 0; x < numRequestsPerActor; x++ {
		for y := 0; y < numSubRequests; y++ {
			r := &req{
				duration:   d,
				queryID:    x,
				subQueryID: y,
				tenant:     tenant,
				actor:      serializedActor,
			}

			if !useActor {
				actor = nil
			}
			err := queue.Enqueue("tenant", actor, r, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestQueryFairness(t *testing.T) {
	numSubRequestsActorA, numSubRequestsActorB := 100, 20
	total := int64((numSubRequestsActorA + numSubRequestsActorB) * numRequestsPerActor)

	m := &Metrics{
		QueueLength:       prometheus.NewGaugeVec(prometheus.GaugeOpts{}, []string{"user"}),
		DiscardedRequests: prometheus.NewCounterVec(prometheus.CounterOpts{}, []string{"user"}),
	}

	for _, useActor := range []bool{false, true} {
		t.Run(fmt.Sprintf("use hierarchical queues = %v", useActor), func(t *testing.T) {
			requestQueue := NewRequestQueue(1024, 0, m)
			enqueueRequestsForActor(t, []string{"a"}, useActor, requestQueue, numSubRequestsActorA, 100*time.Millisecond)
			enqueueRequestsForActor(t, []string{"b"}, useActor, requestQueue, numSubRequestsActorB, 50*time.Millisecond)
			requestQueue.queues.recomputeUserQueriers()

			// set timeout to minize impact on overall test run duration in case something goes wrong
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			start := time.Now()
			durations := &sync.Map{}
			var wg sync.WaitGroup
			var responseCount atomic.Int64
			// Simulate querier loop
			for q := 0; q < numQueriers; q++ {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()

					requestQueue.RegisterQuerierConnection(id)
					defer requestQueue.UnregisterQuerierConnection(id)
					idx := StartIndex
					for ctx.Err() == nil {
						r, newIdx, err := requestQueue.Dequeue(ctx, idx, id)
						res, ok := r.(*req)
						if err != nil || !ok {
							break
						}
						idx = newIdx
						time.Sleep(res.duration)
						count := responseCount.Add(1)
						durations.Store(res.actor, time.Since(start))
						if count == total {
							cancel()
						}
					}
				}(fmt.Sprintf("querier-%d", q))
			}

			wg.Wait()

			require.Equal(t, total, responseCount.Load())
			durations.Range(func(k, v any) bool {
				t.Log("duration actor", k, v)
				return true
			})
			t.Log("total duration", time.Since(start))
		})
	}

}

func TestQueryFairnessAcrossSameLevel(t *testing.T) {
	/**

	`tenant1`, `tenant1|abc`, and `tenant1|xyz` have equal preference
	`tenant1|xyz|123` and `tenant1|xyz|456` have equal preference

	root:
	  tenant1: [0, 1, 2]
		  abc: [10, 11, 12]
			xyz: [20]
			  123: [200]
			  456: [210]
	**/

	m := &Metrics{
		QueueLength:       prometheus.NewGaugeVec(prometheus.GaugeOpts{}, []string{"user"}),
		DiscardedRequests: prometheus.NewCounterVec(prometheus.CounterOpts{}, []string{"user"}),
	}

	requestQueue := NewRequestQueue(1024, 0, m)
	_ = requestQueue.Enqueue("tenant1", []string{}, r(0), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{}, r(1), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{}, r(2), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"abc"}, r(10), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"abc"}, r(11), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"abc"}, r(12), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"xyz"}, r(20), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"xyz", "123"}, r(200), 0, nil)
	_ = requestQueue.Enqueue("tenant1", []string{"xyz", "456"}, r(210), 0, nil)
	requestQueue.queues.recomputeUserQueriers()

	// set timeout to minize impact on overall test run duration in case something goes wrong
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	items := make([]int, 0)
	requestQueue.RegisterQuerierConnection("querier")
	defer requestQueue.UnregisterQuerierConnection("querier")

	idx := StartIndexWithLocalQueue
	for ctx.Err() == nil && len(items) < 9 {
		r, newIdx, err := requestQueue.Dequeue(ctx, idx, "querier")
		res, ok := r.(*dummyRequest)
		if err != nil || !ok {
			break
		}
		idx = newIdx
		items = append(items, res.id)
	}

	require.Equal(t, []int{0, 10, 20, 1, 11, 200, 2, 12, 210}, items)
}
