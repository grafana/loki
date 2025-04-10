package ttlcache

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Available eviction reasons.
const (
	EvictionReasonDeleted EvictionReason = iota + 1
	EvictionReasonCapacityReached
	EvictionReasonExpired
)

// EvictionReason is used to specify why a certain item was
// evicted/deleted.
type EvictionReason int

// Cache is a synchronised map of items that are automatically removed
// when they expire or the capacity is reached.
type Cache[K comparable, V any] struct {
	items struct {
		mu     sync.RWMutex
		values map[K]*list.Element

		// a generic doubly linked list would be more convenient
		// (and more performant?). It's possible that this
		// will be introduced with/in go1.19+
		lru      *list.List
		expQueue expirationQueue[K, V]

		timerCh chan time.Duration
	}

	metricsMu sync.RWMutex
	metrics   Metrics

	events struct {
		insertion struct {
			mu     sync.RWMutex
			nextID uint64
			fns    map[uint64]func(*Item[K, V])
		}
		eviction struct {
			mu     sync.RWMutex
			nextID uint64
			fns    map[uint64]func(EvictionReason, *Item[K, V])
		}
	}

	stopCh  chan struct{}
	options options[K, V]
}

// New creates a new instance of cache.
func New[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	c := &Cache[K, V]{
		stopCh: make(chan struct{}),
	}
	c.items.values = make(map[K]*list.Element)
	c.items.lru = list.New()
	c.items.expQueue = newExpirationQueue[K, V]()
	c.items.timerCh = make(chan time.Duration, 1) // buffer is important
	c.events.insertion.fns = make(map[uint64]func(*Item[K, V]))
	c.events.eviction.fns = make(map[uint64]func(EvictionReason, *Item[K, V]))

	applyOptions(&c.options, opts...)

	return c
}

// updateExpirations updates the expiration queue and notifies
// the cache auto cleaner if needed.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) updateExpirations(fresh bool, elem *list.Element) {
	var oldExpiresAt time.Time

	if !c.items.expQueue.isEmpty() {
		oldExpiresAt = c.items.expQueue[0].Value.(*Item[K, V]).expiresAt
	}

	if fresh {
		c.items.expQueue.push(elem)
	} else {
		c.items.expQueue.update(elem)
	}

	newExpiresAt := c.items.expQueue[0].Value.(*Item[K, V]).expiresAt

	// check if the closest/soonest expiration timestamp changed
	if newExpiresAt.IsZero() || (!oldExpiresAt.IsZero() && !newExpiresAt.Before(oldExpiresAt)) {
		return
	}

	d := time.Until(newExpiresAt)

	// It's possible that the auto cleaner isn't active or
	// is busy, so we need to drain the channel before
	// sending a new value.
	// Also, since this method is called after locking the items' mutex,
	// we can be sure that there is no other concurrent call of this
	// method
	if len(c.items.timerCh) > 0 {
		// we need to drain this channel in a select with a default
		// case because it's possible that the auto cleaner
		// read this channel just after we entered this if
		select {
		case d1 := <-c.items.timerCh:
			if d1 < d {
				d = d1
			}
		default:
		}
	}

	// since the channel has a size 1 buffer, we can be sure
	// that the line below won't block (we can't overfill the buffer
	// because we just drained it)
	c.items.timerCh <- d
}

// set creates a new item, adds it to the cache and then returns it.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) set(key K, value V, ttl time.Duration) *Item[K, V] {
	if ttl == DefaultTTL {
		ttl = c.options.ttl
	}

	elem := c.get(key, false, true)
	if elem != nil {
		// update/overwrite an existing item
		item := elem.Value.(*Item[K, V])
		item.update(value, ttl)
		c.updateExpirations(false, elem)

		return item
	}

	if c.options.capacity != 0 && uint64(len(c.items.values)) >= c.options.capacity {
		// delete the oldest item
		c.evict(EvictionReasonCapacityReached, c.items.lru.Back())
	}

	if ttl == PreviousOrDefaultTTL {
		ttl = c.options.ttl
	}

	// create a new item
	item := newItem(key, value, ttl, c.options.enableVersionTracking)
	elem = c.items.lru.PushFront(item)
	c.items.values[key] = elem
	c.updateExpirations(true, elem)

	c.metricsMu.Lock()
	c.metrics.Insertions++
	c.metricsMu.Unlock()

	c.events.insertion.mu.RLock()
	for _, fn := range c.events.insertion.fns {
		fn(item)
	}
	c.events.insertion.mu.RUnlock()

	return item
}

// get retrieves an item from the cache and extends its expiration
// time if 'touch' is set to true.
// It returns nil if the item is not found or is expired.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) get(key K, touch bool, includeExpired bool) *list.Element {
	elem := c.items.values[key]
	if elem == nil {
		return nil
	}

	item := elem.Value.(*Item[K, V])
	if !includeExpired && item.isExpiredUnsafe() {
		return nil
	}

	c.items.lru.MoveToFront(elem)

	if touch && item.ttl > 0 {
		item.touch()
		c.updateExpirations(false, elem)
	}

	return elem
}

// getWithOpts wraps the get method, applies the given options, and updates
// the metrics.
// It returns nil if the item is not found or is expired.
// If 'lockAndLoad' is set to true, the mutex is locked before calling the
// get method and unlocked after it returns. It also indicates that the
// loader should be used to load external data when the get method returns
// a nil value and the mutex is unlocked.
// If 'lockAndLoad' is set to false, neither the mutex nor the loader is
// used.
func (c *Cache[K, V]) getWithOpts(key K, lockAndLoad bool, opts ...Option[K, V]) *Item[K, V] {
	getOpts := options[K, V]{
		loader:            c.options.loader,
		disableTouchOnHit: c.options.disableTouchOnHit,
	}

	applyOptions(&getOpts, opts...)

	if lockAndLoad {
		c.items.mu.Lock()
	}

	elem := c.get(key, !getOpts.disableTouchOnHit, false)

	if lockAndLoad {
		c.items.mu.Unlock()
	}

	if elem == nil {
		c.metricsMu.Lock()
		c.metrics.Misses++
		c.metricsMu.Unlock()

		if lockAndLoad && getOpts.loader != nil {
			return getOpts.loader.Load(c, key)
		}

		return nil
	}

	c.metricsMu.Lock()
	c.metrics.Hits++
	c.metricsMu.Unlock()

	return elem.Value.(*Item[K, V])
}

// evict deletes items from the cache.
// If no items are provided, all currently present cache items
// are evicted.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) evict(reason EvictionReason, elems ...*list.Element) {
	if len(elems) > 0 {
		c.metricsMu.Lock()
		c.metrics.Evictions += uint64(len(elems))
		c.metricsMu.Unlock()

		c.events.eviction.mu.RLock()
		for i := range elems {
			item := elems[i].Value.(*Item[K, V])
			delete(c.items.values, item.key)
			c.items.lru.Remove(elems[i])
			c.items.expQueue.remove(elems[i])

			for _, fn := range c.events.eviction.fns {
				fn(reason, item)
			}
		}
		c.events.eviction.mu.RUnlock()

		return
	}

	c.metricsMu.Lock()
	c.metrics.Evictions += uint64(len(c.items.values))
	c.metricsMu.Unlock()

	c.events.eviction.mu.RLock()
	for _, elem := range c.items.values {
		item := elem.Value.(*Item[K, V])

		for _, fn := range c.events.eviction.fns {
			fn(reason, item)
		}
	}
	c.events.eviction.mu.RUnlock()

	c.items.values = make(map[K]*list.Element)
	c.items.lru.Init()
	c.items.expQueue = newExpirationQueue[K, V]()
}

// delete deletes an item by the provided key.
// The method is no-op if the item is not found.
// Not safe for concurrent use by multiple goroutines without additional
// locking.
func (c *Cache[K, V]) delete(key K) {
	elem := c.items.values[key]
	if elem == nil {
		return
	}

	c.evict(EvictionReasonDeleted, elem)
}

// Set creates a new item from the provided key and value, adds
// it to the cache and then returns it. If an item associated with the
// provided key already exists, the new item overwrites the existing one.
// NoTTL constant or -1 can be used to indicate that the item should never
// expire.
// DefaultTTL constant or 0 can be used to indicate that the item should use
// the default/global TTL that was specified when the cache instance was
// created.
func (c *Cache[K, V]) Set(key K, value V, ttl time.Duration) *Item[K, V] {
	c.items.mu.Lock()
	defer c.items.mu.Unlock()

	return c.set(key, value, ttl)
}

// Get retrieves an item from the cache by the provided key.
// Unless this is disabled, it also extends/touches an item's
// expiration timestamp on successful retrieval.
// If the item is not found, a nil value is returned.
func (c *Cache[K, V]) Get(key K, opts ...Option[K, V]) *Item[K, V] {
	return c.getWithOpts(key, true, opts...)
}

// Delete deletes an item from the cache. If the item associated with
// the key is not found, the method is no-op.
func (c *Cache[K, V]) Delete(key K) {
	c.items.mu.Lock()
	defer c.items.mu.Unlock()

	c.delete(key)
}

// Has checks whether the key exists in the cache.
func (c *Cache[K, V]) Has(key K) bool {
	c.items.mu.RLock()
	defer c.items.mu.RUnlock()

	elem, ok := c.items.values[key]
	return ok && !elem.Value.(*Item[K, V]).isExpiredUnsafe()
}

// GetOrSet retrieves an item from the cache by the provided key.
// If the item is not found, it is created with the provided options and
// then returned.
// The bool return value is true if the item was found, false if created
// during the execution of the method.
// If the loader is non-nil (i.e., used as an option or specified when
// creating the cache instance), its execution is skipped.
func (c *Cache[K, V]) GetOrSet(key K, value V, opts ...Option[K, V]) (*Item[K, V], bool) {
	c.items.mu.Lock()
	defer c.items.mu.Unlock()

	elem := c.getWithOpts(key, false, opts...)
	if elem != nil {
		return elem, true
	}

	setOpts := options[K, V]{
		ttl: c.options.ttl,
	}
	applyOptions(&setOpts, opts...) // used only to update the TTL

	item := c.set(key, value, setOpts.ttl)

	return item, false
}

// GetAndDelete retrieves an item from the cache by the provided key and
// then deletes it.
// The bool return value is true if the item was found before
// its deletion, false if not.
// If the loader is non-nil (i.e., used as an option or specified when
// creating the cache instance), it is executed normaly, i.e., only when
// the item is not found.
func (c *Cache[K, V]) GetAndDelete(key K, opts ...Option[K, V]) (*Item[K, V], bool) {
	c.items.mu.Lock()

	elem := c.getWithOpts(key, false, opts...)
	if elem == nil {
		c.items.mu.Unlock()

		getOpts := options[K, V]{
			loader: c.options.loader,
		}
		applyOptions(&getOpts, opts...) // used only to update the loader

		if getOpts.loader != nil {
			item := getOpts.loader.Load(c, key)
			return item, item != nil
		}

		return nil, false
	}

	c.delete(key)
	c.items.mu.Unlock()

	return elem, true
}

// DeleteAll deletes all items from the cache.
func (c *Cache[K, V]) DeleteAll() {
	c.items.mu.Lock()
	c.evict(EvictionReasonDeleted)
	c.items.mu.Unlock()
}

// DeleteExpired deletes all expired items from the cache.
func (c *Cache[K, V]) DeleteExpired() {
	c.items.mu.Lock()
	defer c.items.mu.Unlock()

	if c.items.expQueue.isEmpty() {
		return
	}

	e := c.items.expQueue[0]
	for e.Value.(*Item[K, V]).isExpiredUnsafe() {
		c.evict(EvictionReasonExpired, e)

		if c.items.expQueue.isEmpty() {
			break
		}

		// expiration queue has a new root
		e = c.items.expQueue[0]
	}
}

// Touch simulates an item's retrieval without actually returning it.
// Its main purpose is to extend an item's expiration timestamp.
// If the item is not found, the method is no-op.
func (c *Cache[K, V]) Touch(key K) {
	c.items.mu.Lock()
	c.get(key, true, false)
	c.items.mu.Unlock()
}

// Len returns the number of unexpired items in the cache.
func (c *Cache[K, V]) Len() int {
	c.items.mu.RLock()
	defer c.items.mu.RUnlock()

	total := c.items.expQueue.Len()
	if total == 0 {
		return 0
	}

	// search the heap-based expQueue by BFS
	countExpired := func() int {
		var (
			q   []int
			res int
		)

		item := c.items.expQueue[0].Value.(*Item[K, V])
		if !item.isExpiredUnsafe() {
			return res
		}

		q = append(q, 0)
		for len(q) > 0 {
			pop := q[0]
			q = q[1:]
			res++

			for i := 1; i <= 2; i++ {
				idx := 2*pop + i
				if idx >= total {
					break
				}

				item = c.items.expQueue[idx].Value.(*Item[K, V])
				if item.isExpiredUnsafe() {
					q = append(q, idx)
				}
			}
		}
		return res
	}

	return total - countExpired()
}

// Keys returns all unexpired keys in the cache.
func (c *Cache[K, V]) Keys() []K {
	c.items.mu.RLock()
	defer c.items.mu.RUnlock()

	res := make([]K, 0)
	for k, elem := range c.items.values {
		if !elem.Value.(*Item[K, V]).isExpiredUnsafe() {
			res = append(res, k)
		}
	}

	return res
}

// Items returns a copy of all items in the cache.
// It does not update any expiration timestamps.
func (c *Cache[K, V]) Items() map[K]*Item[K, V] {
	c.items.mu.RLock()
	defer c.items.mu.RUnlock()

	items := make(map[K]*Item[K, V])
	for k, elem := range c.items.values {
		item := elem.Value.(*Item[K, V])
		if item != nil && !item.isExpiredUnsafe() {
			items[k] = item
		}
	}

	return items
}

// Range calls fn for each unexpired item in the cache. If fn returns false,
// Range stops the iteration.
func (c *Cache[K, V]) Range(fn func(item *Item[K, V]) bool) {
	c.items.mu.RLock()

	// Check if cache is empty
	if c.items.lru.Len() == 0 {
		c.items.mu.RUnlock()
		return
	}

	for item := c.items.lru.Front(); item != c.items.lru.Back().Next(); item = item.Next() {
		i := item.Value.(*Item[K, V])
		expired := i.isExpiredUnsafe()
		c.items.mu.RUnlock()

		if !expired && !fn(i) {
			return
		}

		if item.Next() != nil {
			c.items.mu.RLock()
		}
	}
}

// RangeBackwards calls fn for each unexpired item in the cache in reverse order.
// If fn returns false, RangeBackwards stops the iteration.
func (c *Cache[K, V]) RangeBackwards(fn func(item *Item[K, V]) bool) {
	c.items.mu.RLock()

	// Check if cache is empty
	if c.items.lru.Len() == 0 {
		c.items.mu.RUnlock()
		return
	}

	for item := c.items.lru.Back(); item != c.items.lru.Front().Prev(); item = item.Prev() {
		i := item.Value.(*Item[K, V])
		expired := i.isExpiredUnsafe()
		c.items.mu.RUnlock()

		if !expired && !fn(i) {
			return
		}

		if item.Prev() != nil {
			c.items.mu.RLock()
		}
	}
}

// Metrics returns the metrics of the cache.
func (c *Cache[K, V]) Metrics() Metrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	return c.metrics
}

// Start starts an automatic cleanup process that periodically deletes
// expired items.
// It blocks until Stop is called.
func (c *Cache[K, V]) Start() {
	waitDur := func() time.Duration {
		c.items.mu.RLock()
		defer c.items.mu.RUnlock()

		if !c.items.expQueue.isEmpty() &&
			!c.items.expQueue[0].Value.(*Item[K, V]).expiresAt.IsZero() {
			d := time.Until(c.items.expQueue[0].Value.(*Item[K, V]).expiresAt)
			if d <= 0 {
				// execute immediately
				return time.Microsecond
			}

			return d
		}

		if c.options.ttl > 0 {
			return c.options.ttl
		}

		return time.Hour
	}

	timer := time.NewTimer(waitDur())
	stop := func() {
		if !timer.Stop() {
			// drain the timer chan
			select {
			case <-timer.C:
			default:
			}
		}
	}

	defer stop()

	for {
		select {
		case <-c.stopCh:
			return
		case d := <-c.items.timerCh:
			stop()
			timer.Reset(d)
		case <-timer.C:
			c.DeleteExpired()
			stop()
			timer.Reset(waitDur())
		}
	}
}

// Stop stops the automatic cleanup process.
// It blocks until the cleanup process exits.
func (c *Cache[K, V]) Stop() {
	c.stopCh <- struct{}{}
}

// OnInsertion adds the provided function to be executed when
// a new item is inserted into the cache. The function is executed
// on a separate goroutine and does not block the flow of the cache
// manager.
// The returned function may be called to delete the subscription function
// from the list of insertion subscribers.
// When the returned function is called, it blocks until all instances of
// the same subscription function return. A context is used to notify the
// subscription function when the returned/deletion function is called.
func (c *Cache[K, V]) OnInsertion(fn func(context.Context, *Item[K, V])) func() {
	var (
		wg          sync.WaitGroup
		ctx, cancel = context.WithCancel(context.Background())
	)

	c.events.insertion.mu.Lock()
	id := c.events.insertion.nextID
	c.events.insertion.fns[id] = func(item *Item[K, V]) {
		wg.Add(1)
		go func() {
			fn(ctx, item)
			wg.Done()
		}()
	}
	c.events.insertion.nextID++
	c.events.insertion.mu.Unlock()

	return func() {
		cancel()

		c.events.insertion.mu.Lock()
		delete(c.events.insertion.fns, id)
		c.events.insertion.mu.Unlock()

		wg.Wait()
	}
}

// OnEviction adds the provided function to be executed when
// an item is evicted/deleted from the cache. The function is executed
// on a separate goroutine and does not block the flow of the cache
// manager.
// The returned function may be called to delete the subscription function
// from the list of eviction subscribers.
// When the returned function is called, it blocks until all instances of
// the same subscription function return. A context is used to notify the
// subscription function when the returned/deletion function is called.
func (c *Cache[K, V]) OnEviction(fn func(context.Context, EvictionReason, *Item[K, V])) func() {
	var (
		wg          sync.WaitGroup
		ctx, cancel = context.WithCancel(context.Background())
	)

	c.events.eviction.mu.Lock()
	id := c.events.eviction.nextID
	c.events.eviction.fns[id] = func(r EvictionReason, item *Item[K, V]) {
		wg.Add(1)
		go func() {
			fn(ctx, r, item)
			wg.Done()
		}()
	}
	c.events.eviction.nextID++
	c.events.eviction.mu.Unlock()

	return func() {
		cancel()

		c.events.eviction.mu.Lock()
		delete(c.events.eviction.fns, id)
		c.events.eviction.mu.Unlock()

		wg.Wait()
	}
}

// Loader is an interface that handles missing data loading.
type Loader[K comparable, V any] interface {
	// Load should execute a custom item retrieval logic and
	// return the item that is associated with the key.
	// It should return nil if the item is not found/valid.
	// The method is allowed to fetch data from the cache instance
	// or update it for future use.
	Load(c *Cache[K, V], key K) *Item[K, V]
}

// LoaderFunc type is an adapter that allows the use of ordinary
// functions as data loaders.
type LoaderFunc[K comparable, V any] func(*Cache[K, V], K) *Item[K, V]

// Load executes a custom item retrieval logic and returns the item that
// is associated with the key.
// It returns nil if the item is not found/valid.
func (l LoaderFunc[K, V]) Load(c *Cache[K, V], key K) *Item[K, V] {
	return l(c, key)
}

// SuppressedLoader wraps another Loader and suppresses duplicate
// calls to its Load method.
type SuppressedLoader[K comparable, V any] struct {
	loader Loader[K, V]
	group  *singleflight.Group
}

// NewSuppressedLoader creates a new instance of suppressed loader.
// If the group parameter is nil, a newly created instance of
// *singleflight.Group is used.
func NewSuppressedLoader[K comparable, V any](loader Loader[K, V], group *singleflight.Group) *SuppressedLoader[K, V] {
	if group == nil {
		group = &singleflight.Group{}
	}

	return &SuppressedLoader[K, V]{
		loader: loader,
		group:  group,
	}
}

// Load executes a custom item retrieval logic and returns the item that
// is associated with the key.
// It returns nil if the item is not found/valid.
// It also ensures that only one execution of the wrapped Loader's Load
// method is in-flight for a given key at a time.
func (l *SuppressedLoader[K, V]) Load(c *Cache[K, V], key K) *Item[K, V] {
	// there should be a better/generic way to create a
	// singleflight Group's key. It's possible that a generic
	// singleflight.Group will be introduced with/in go1.19+
	strKey := fmt.Sprint(key)

	// the error can be discarded since the singleflight.Group
	// itself does not return any of its errors, it returns
	// the error that we return ourselves in the func below, which
	// is also nil
	res, _, _ := l.group.Do(strKey, func() (interface{}, error) {
		item := l.loader.Load(c, key)
		if item == nil {
			return nil, nil
		}

		return item, nil
	})
	if res == nil {
		return nil
	}

	return res.(*Item[K, V])
}
