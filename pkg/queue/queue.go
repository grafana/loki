package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"go.uber.org/atomic"
)

const (
	// How frequently to check for disconnected queriers that should be forgotten.
	forgetCheckPeriod = 5 * time.Second
	anyQueue          = ""
)

var (
	ErrTooManyRequests = errors.New("too many outstanding requests")
	ErrStopped         = errors.New("queue is stopped")
	ErrQueueWasRemoved = errors.New("the queue has been removed or moved to another position")
)

// QueueIndex is opaque type that allows to resume iteration over tenants between successive calls
// of RequestQueue.GetNextRequestForQuerier method.
type QueueIndex int // nolint:revive

// StartIndexWithLocalQueue is the index of the queue that starts iteration over local and sub queues.
var StartIndexWithLocalQueue QueueIndex = -2

// StartIndex is the index of the queue that starts iteration over sub queues.
var StartIndex QueueIndex = -1

// Modify index to start iteration on the same tenant, for which last queue was returned.
func (ui QueueIndex) ReuseLastIndex() QueueIndex {
	if ui < StartIndex {
		return ui
	}
	return ui - 1
}

type Limits interface {
	// MaxConsumers returns the max consumers to use per tenant or 0 to allow all consumers to consume from the queue.
	MaxConsumers(user string, allConsumers int) int
}

// Request stored into the queue.
type Request any

// RequestChannel is a channel that queues Requests
type RequestChannel chan Request

// RequestQueue holds incoming requests in per-tenant queues. It also assigns each tenant specified number of queriers,
// and when querier asks for next request to handle (using GetNextRequestForQuerier), it returns requests
// in a fair fashion.
type RequestQueue struct {
	services.Service

	connectedConsumers *atomic.Int32

	mtx     sync.Mutex
	cond    contextCond // Notified when request is enqueued or dequeued, or querier is disconnected.
	queues  *tenantQueues
	stopped bool

	metrics *Metrics
	pool    *SlicePool[Request]
}

func NewRequestQueue(maxOutstandingPerTenant int, forgetDelay time.Duration, limits Limits, metrics *Metrics) *RequestQueue {
	q := &RequestQueue{
		queues:             newTenantQueues(maxOutstandingPerTenant, forgetDelay, limits),
		connectedConsumers: atomic.NewInt32(0),
		metrics:            metrics,
		pool:               NewSlicePool[Request](1<<6, 1<<10, 2), // Buckets are [64, 128, 256, 512, 1024].
	}

	q.cond = contextCond{Cond: sync.NewCond(&q.mtx)}
	q.Service = services.NewTimerService(forgetCheckPeriod, nil, q.forgetDisconnectedConsumers, q.stopping).WithName("request queue")

	return q
}

// Enqueue puts the request into the queue.
// If request is successfully enqueued, successFn is called with the lock held, before any querier can receive the request.
func (q *RequestQueue) Enqueue(tenant string, path []string, req Request, successFn func()) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	if q.stopped {
		return ErrStopped
	}

	queue, err := q.queues.getOrAddQueue(tenant, path)
	if err != nil {
		return fmt.Errorf("no queue found: %w", err)
	}

	// Optimistically increase queue counter for tenant instead of doing separate
	// get and set operations, because _most_ of the time the increased value is
	// smaller than the max queue length.
	// We need to keep track of queue length separately because the size of the
	// buffered channel is the same across all sub-queues which would allow
	// enqueuing more items than there are allowed at tenant level.
	queueLen := q.queues.perUserQueueLen.Inc(tenant)
	if queueLen > q.queues.maxUserQueueSize {
		q.metrics.discardedRequests.WithLabelValues(tenant).Inc()
		// decrement, because we already optimistically increased the counter
		q.queues.perUserQueueLen.Dec(tenant)
		return ErrTooManyRequests
	}

	select {
	case queue.Chan() <- req:
		q.metrics.queueLength.WithLabelValues(tenant).Inc()
		q.metrics.enqueueCount.WithLabelValues(tenant, fmt.Sprint(len(path))).Inc()
		q.cond.Broadcast()
		// Call this function while holding a lock. This guarantees that no querier can fetch the request before function returns.
		if successFn != nil {
			successFn()
		}
		return nil
	default:
		q.metrics.discardedRequests.WithLabelValues(tenant).Inc()
		// decrement, because we already optimistically increased the counter
		q.queues.perUserQueueLen.Dec(tenant)
		return ErrTooManyRequests
	}
}

// ReleaseRequests returns items back to the slice pool.
// Must only be called in combination with DequeueMany().
func (q *RequestQueue) ReleaseRequests(items []Request) {
	q.pool.Put(items)
}

// DequeueMany consumes multiple items for a single tenant from the queue.
// It blocks the execution until it dequeues at least 1 request and continue reading
// until it reaches `maxItems` requests or if no requests for this tenant are enqueued.
// The caller is responsible for returning the dequeued requests back to the
// pool by calling ReleaseRequests(items).
func (q *RequestQueue) DequeueMany(ctx context.Context, idx QueueIndex, consumerID string, maxItems int) ([]Request, QueueIndex, error) {
	items := q.pool.Get(maxItems)
	lastQueueName := anyQueue
	for {
		item, newIdx, newQueueName, isTenantQueueEmpty, err := q.dequeue(ctx, idx, lastQueueName, consumerID)
		if err != nil {
			// the consumer must receive the items if tenants queue is removed,
			// even if it has collected less than `maxItems` requests.
			if errors.Is(err, ErrQueueWasRemoved) {
				err = nil
			}
			return items, newIdx, err
		}
		lastQueueName = newQueueName
		items = append(items, item)
		idx = newIdx.ReuseLastIndex()
		if len(items) == maxItems || isTenantQueueEmpty {
			return items, newIdx, nil
		}
	}
}

// Dequeue find next tenant queue and takes the next request off of it. Will block if there are no requests.
// By passing tenant index from previous call of this method, querier guarantees that it iterates over all tenants fairly.
// Even if the consumer used UserIndex.ReuseLastUser to fetch the request from the same tenant's queue, it does not provide
// any guaranties that the previously used queue is still at this position because another consumer could already read
// the last request and the queue could be removed and another queue is already placed at this position.
func (q *RequestQueue) Dequeue(ctx context.Context, last QueueIndex, consumerID string) (Request, QueueIndex, error) {
	dequeue, queueIndex, _, _, err := q.dequeue(ctx, last, anyQueue, consumerID)
	return dequeue, queueIndex, err
}

func (q *RequestQueue) dequeue(ctx context.Context, last QueueIndex, wantedQueueName string, consumerID string) (Request, QueueIndex, string, bool, error) {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	querierWait := false

FindQueue:
	// We need to wait if there are no tenants, or no pending requests for given querier.
	// However, if `wantedQueueName` is not empty, the caller must not be blocked because it wants to read exactly from that queue, not others.
	for (q.queues.hasNoTenantQueues() || querierWait) && ctx.Err() == nil && !q.stopped && wantedQueueName == anyQueue {
		querierWait = false
		q.cond.Wait(ctx)
	}

	// If the current consumer wants to read from specific queue, but he does not have any queues available for him,
	// return an error to notify that queue has been already removed.
	if q.queues.hasNoTenantQueues() && wantedQueueName != anyQueue {
		return nil, last, wantedQueueName, false, ErrQueueWasRemoved
	}

	if q.stopped {
		return nil, last, wantedQueueName, false, ErrStopped
	}

	if err := ctx.Err(); err != nil {
		return nil, last, wantedQueueName, false, err
	}

	queue, tenant, idx := q.queues.getNextQueueForConsumer(last, consumerID)
	last = idx
	if queue == nil {
		// it can be a case the consumer has other tenants queues available for him,
		// it allows the consumer to pass the wait block,
		// but the queue with index `last+1` has been already removed,
		// for example if another consumer has read the last request from that queue,
		// and as long as this consumer wants to read from specific tenant queue,
		// it's necessary to return `ErrQueueWasRemoved` error.
		if wantedQueueName != anyQueue {
			return nil, last, wantedQueueName, false, ErrQueueWasRemoved
		}
		// otherwise, if wantedQueueName is empty, then this consumer will go to the wait block again
		// and as long as `last` index is updated, next time the consumer will request the queue
		// with the new index that was returned from `getNextQueueForConsumer`.
		// There are no unexpired requests, so we can get back
		// and wait for more requests.
		querierWait = true
		goto FindQueue
	}

	if wantedQueueName != anyQueue && wantedQueueName != queue.Name() {
		// it means that the consumer received another tenants queue because it was already removed
		// or another queue is already at this index
		return nil, last, queue.Name(), false, ErrQueueWasRemoved
	}
	// Pick next request from the queue.
	request := queue.Dequeue()
	isTenantQueueEmpty := queue.Len() == 0
	if isTenantQueueEmpty {
		q.queues.deleteQueue(tenant)
	}

	q.queues.perUserQueueLen.Dec(tenant)
	q.metrics.queueLength.WithLabelValues(tenant).Dec()

	// Tell close() we've processed a request.
	q.cond.Broadcast()

	return request, last, queue.Name(), isTenantQueueEmpty, nil
}

func (q *RequestQueue) forgetDisconnectedConsumers(_ context.Context) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	if q.queues.forgetDisconnectedConsumers(time.Now()) > 0 {
		// We need to notify goroutines cause having removed some queriers
		// may have caused a resharding.
		q.cond.Broadcast()
	}

	return nil
}

func (q *RequestQueue) stopping(_ error) error {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	for !q.queues.hasNoTenantQueues() && q.connectedConsumers.Load() > 0 {
		q.cond.Wait(context.Background())
	}

	// Only stop after dispatching enqueued requests.
	q.stopped = true

	// If there are still goroutines in GetNextRequestForQuerier method, they get notified.
	q.cond.Broadcast()

	return nil
}

func (q *RequestQueue) RegisterConsumerConnection(querier string) {
	q.connectedConsumers.Inc()

	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.addConsumerToConnection(querier)
}

func (q *RequestQueue) UnregisterConsumerConnection(querier string) {
	q.connectedConsumers.Dec()

	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.removeConsumerConnection(querier, time.Now())
}

func (q *RequestQueue) NotifyConsumerShutdown(querierID string) {
	q.mtx.Lock()
	defer q.mtx.Unlock()
	q.queues.notifyQuerierShutdown(querierID)
}

func (q *RequestQueue) GetConnectedConsumersMetric() float64 {
	return float64(q.connectedConsumers.Load())
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
