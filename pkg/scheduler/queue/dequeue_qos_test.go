package queue

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
						// debug logging
						// t.Log(id, res, err, count)
						durations.Store(res.actor, time.Since(start))
						if count == int64((numSubRequestsActorA+numSubRequestsActorB)*numRequestsPerActor) {
							cancel()
						}
					}
				}(fmt.Sprintf("querier-%d", q))
			}

			wg.Wait()

			durations.Range(func(k, v any) bool {
				t.Log("duration actor", k, v)
				return true
			})
			t.Log("total duration", time.Since(start))
		})
	}

}
