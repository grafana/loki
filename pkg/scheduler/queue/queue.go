package queue

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
)

const (
	// How frequently to check for disconnected queriers that should be forgotten.
	forgetCheckPeriod = 5 * time.Second
)

var (
	ErrTooManyRequests = errors.New("too many outstanding requests")
	ErrStopped         = errors.New("queue is stopped")
)

// TenantIndex is opaque type that allows to resume iteration over tenants between successive calls
// of RequestQueue.GetNextRequestForQuerier method.
type TenantIndex int

// Modify index to start iteration on the same tenant, for which last queue was returned.
func (ui TenantIndex) ReuseLastTenant() TenantIndex {
	if ui < 0 {
		return ui
	}
	return ui - 1
}

// FirstTenant is the UserIndex that starts iteration over tenant queues from the very first tenant.
var FirstTenant TenantIndex = -1

// Request stored into the queue.
type Request any

// RequestChannel is a channel that queues Requests
type RequestChannel chan Request

// RequestQueue holds incoming requests in per-tenant queues. It also assigns each tenant specified number of queriers,
// and when querier asks for next request to handle (using GetNextRequestForQuerier), it returns requests
// in a fair fashion.
type RequestQueue struct {
	services.Service

	connectedQuerierWorkers *atomic.Int32

	mtx     sync.Mutex
	cond    contextCond // Notified when request is enqueued or dequeued, or querier is disconnected.
	queues  *tenantQueues
	stopped bool

	queueLength       *prometheus.GaugeVec   // Per tenant and reason.
	discardedRequests *prometheus.CounterVec // Per tenant.
}

func NewRequestQueue(maxOutstandingPerTenant int, forgetDelay time.Duration, queueLength *prometheus.GaugeVec, discardedRequests *prometheus.CounterVec) *RequestQueue {
	q := &RequestQueue{
		queues:                  newTenantQueues(maxOutstandingPerTenant, forgetDelay),
		connectedQuerierWorkers: atomic.NewInt32(0),
		queueLength:             queueLength,
		discardedRequests:       discardedRequests,
	}

	q.cond = contextCond{Cond: sync.NewCond(&q.mtx)}
	q.Service = services.NewTimerService(forgetCheckPeriod, nil, q.forgetDisconnectedQueriers, q.stopping).WithName("request queue")

	return q
}

// EnqueueRequest puts the request into the queue. MaxQueries is tenant-specific value that specifies how many queriers can
// this tenant use (zero or negative = all queriers). It is passed to each EnqueueRequest, because it can change
// between calls.
//
// If request is successfully enqueued, successFn is called with the lock held, before any querier can receive the request.
func (q *RequestQueue) EnqueueRequest(tenant string, req Request, maxQueriers int, successFn func()) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	if q.stopped {
		return ErrStopped
	}

	queue := q.queues.getOrAddQueue(tenant, maxQueriers)
	if queue == nil {
		// This can only happen if tenant is "".
		return errors.New("no queue found")
	}

	select {
	case queue <- req:
		q.queueLength.WithLabelValues(tenant).Inc()
		q.cond.Broadcast()
		// Call this function while holding a lock. This guarantees that no querier can fetch the request before function returns.
		if successFn != nil {
			successFn()
		}
		return nil
	default:
		q.discardedRequests.WithLabelValues(tenant).Inc()
		return ErrTooManyRequests
	}
}

// GetNextRequestForQuerier find next tenant queue and takes the next request off of it. Will block if there are no requests.
// By passing tenant index from previous call of this method, querier guarantees that it iterates over all tenants fairly.
// If querier finds that request from the tenant is already expired, it can get a request for the same tenant by using UserIndex.ReuseLastUser.
func (q *RequestQueue) GetNextRequestForQuerier(ctx context.Context, last TenantIndex, querierID string) (Request, TenantIndex, error) {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	querierWait := false

FindQueue:
	// We need to wait if there are no tenants, or no pending requests for given querier.
	for (q.queues.len() == 0 || querierWait) && ctx.Err() == nil && !q.stopped {
		querierWait = false
		q.cond.Wait(ctx)
	}

	if q.stopped {
		return nil, last, ErrStopped
	}

	if err := ctx.Err(); err != nil {
		return nil, last, err
	}

	for {
		queue, tenant, idx := q.queues.getNextQueueForQuerier(last, querierID)
		last = idx
		if queue == nil {
			break
		}

		// Pick next request from the queue.
		for {
			request := <-queue
			if len(queue) == 0 {
				q.queues.deleteQueue(tenant)
			}

			q.queueLength.WithLabelValues(tenant).Dec()

			// Tell close() we've processed a request.
			q.cond.Broadcast()

			return request, last, nil
		}
	}

	// There are no unexpired requests, so we can get back
	// and wait for more requests.
	querierWait = true
	goto FindQueue
}

func (q *RequestQueue) forgetDisconnectedQueriers(_ context.Context) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	if q.queues.forgetDisconnectedQueriers(time.Now()) > 0 {
		// We need to notify goroutines cause having removed some queriers
		// may have caused a resharding.
		q.cond.Broadcast()
	}

	return nil
}

func (q *RequestQueue) stopping(_ error) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	for q.queues.len() > 0 && q.connectedQuerierWorkers.Load() > 0 {
		q.cond.Wait(context.Background())
	}

	// Only stop after dispatching enqueued requests.
	q.stopped = true

	// If there are still goroutines in GetNextRequestForQuerier method, they get notified.
	q.cond.Broadcast()

	return nil
}

func (q *RequestQueue) RegisterQuerierConnection(querier string) {
	q.connectedQuerierWorkers.Inc()

	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.addQuerierConnection(querier)
}

func (q *RequestQueue) UnregisterQuerierConnection(querier string) {
	q.connectedQuerierWorkers.Dec()

	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.removeQuerierConnection(querier, time.Now())
}

func (q *RequestQueue) NotifyQuerierShutdown(querierID string) {
	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.notifyQuerierShutdown(querierID)
}

func (q *RequestQueue) GetConnectedQuerierWorkersMetric() float64 {
	return float64(q.connectedQuerierWorkers.Load())
}

// contextCond is a *sync.Cond with Wait() method overridden to support context-based waiting.
type contextCond struct {
	*sync.Cond

	// testHookBeforeWaiting is called before calling Cond.Wait() if it's not nil.
	// Yes, it's ugly, but the http package settled jurisprudence:
	// https://github.com/golang/go/blob/6178d25fc0b28724b1b5aec2b1b74fc06d9294c7/src/net/http/client.go#L596-L601
	testHookBeforeWaiting func()
}

// Wait does c.cond.Wait() but will also return if the context provided is done.
// All the documentation of sync.Cond.Wait() applies, but it's especially important to remember that the mutex of
// the cond should be held while Wait() is called (and mutex will be held once it returns)
func (c contextCond) Wait(ctx context.Context) {
	// "condWait" goroutine does q.cond.Wait() and signals through condWait channel.
	condWait := make(chan struct{})
	go func() {
		if c.testHookBeforeWaiting != nil {
			c.testHookBeforeWaiting()
		}
		c.Cond.Wait()
		close(condWait)
	}()

	// "waiting" goroutine: signals that the condWait goroutine has started waiting.
	// Notice that a closed waiting channel implies that the goroutine above has started waiting
	// (because it has unlocked the mutex), but the other way is not true:
	// - condWait it may have unlocked and is waiting, but someone else locked the mutex faster than us:
	//   in this case that caller will eventually unlock, and we'll be able to enter here.
	// - condWait called Wait(), unlocked, received a broadcast and locked again faster than we were able to lock here:
	//   in this case condWait channel will be closed, and this goroutine will be waiting until we unlock.
	waiting := make(chan struct{})
	go func() {
		c.L.Lock()
		close(waiting)
		c.L.Unlock()
	}()

	select {
	case <-condWait:
		// We don't know whether the waiting goroutine is done or not, but we don't care:
		// it will be done once nobody is fighting for the mutex anymore.
	case <-ctx.Done():
		// In order to avoid leaking the condWait goroutine, we can send a broadcast.
		// Before sending the broadcast we need to make sure that condWait goroutine is already waiting (or has already waited).
		select {
		case <-condWait:
			// No need to broadcast as q.cond.Wait() has returned already.
			return
		case <-waiting:
			// q.cond.Wait() might be still waiting (or maybe not!), so we'll poke it just in case.
			c.Broadcast()
		}

		// Make sure we are not waiting anymore, we need to do that before returning as the caller will need to unlock the mutex.
		<-condWait
	}
}
