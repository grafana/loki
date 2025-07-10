package tsdb

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DmitriyVTitov/size"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/index"
)

const (
	// NOTE: keep them exported to reference them in Mimir.

	DefaultPostingsForMatchersCacheTTL      = 10 * time.Second
	DefaultPostingsForMatchersCacheMaxItems = 100
	DefaultPostingsForMatchersCacheMaxBytes = 10 * 1024 * 1024 // DefaultPostingsForMatchersCacheMaxBytes is based on the default max items, 10MB / 100 = 100KB per cached entry on average.
	DefaultPostingsForMatchersCacheForce    = false
)

const (
	evictionReasonTTL = iota
	evictionReasonMaxBytes
	evictionReasonMaxItems
	evictionReasonUnknown
	evictionReasonsLength
)

// IndexPostingsReader is a subset of IndexReader methods, the minimum required to evaluate PostingsForMatchers.
type IndexPostingsReader interface {
	// LabelValues returns possible label values which may not be sorted.
	LabelValues(ctx context.Context, name string, hints *storage.LabelHints, matchers ...*labels.Matcher) ([]string, error)

	// Postings returns the postings list iterator for the label pairs.
	// The Postings here contain the offsets to the series inside the index.
	// Found IDs are not strictly required to point to a valid Series, e.g.
	// during background garbage collections. Input values must be sorted.
	Postings(ctx context.Context, name string, values ...string) (index.Postings, error)

	// PostingsForLabelMatching returns a sorted iterator over postings having a label with the given name and a value for which match returns true.
	// If no postings are found having at least one matching label, an empty iterator is returned.
	PostingsForLabelMatching(ctx context.Context, name string, match func(value string) bool) index.Postings

	// PostingsForAllLabelValues returns a sorted iterator over all postings having a label with the given name.
	// If no postings are found with the label in question, an empty iterator is returned.
	PostingsForAllLabelValues(ctx context.Context, name string) index.Postings
}

// NewPostingsForMatchersCache creates a new PostingsForMatchersCache.
// If `ttl` is 0, then it only deduplicates in-flight requests.
// If `force` is true, then all requests go through cache, regardless of the `concurrent` param provided to the PostingsForMatchers method.
// The `tracingKV` parameter allows passing additional attributes for tracing purposes, which are appended to the default attributes.
func NewPostingsForMatchersCache(ttl time.Duration, maxItems int, maxBytes int64, force bool, metrics *PostingsForMatchersCacheMetrics, tracingKV []attribute.KeyValue) *PostingsForMatchersCache {
	b := &PostingsForMatchersCache{
		calls:            &sync.Map{},
		cached:           list.New(),
		expireInProgress: atomic.NewBool(false),

		ttl:      ttl,
		maxItems: maxItems,
		maxBytes: maxBytes,
		force:    force,
		metrics:  metrics,

		timeNow: func() time.Time {
			// Ensure it is UTC, so that it's faster to compute the cache entry size.
			return time.Now().UTC()
		},
		postingsForMatchers: PostingsForMatchers,
		// Clone the slice so we don't end up sharding it with the parent.
		additionalAttributes: append(slices.Clone(tracingKV), attribute.Stringer("ttl", ttl), attribute.Bool("force", force)),
	}

	return b
}

// PostingsForMatchersCache caches PostingsForMatchers call results when the concurrent hint is passed in or force is true.
type PostingsForMatchersCache struct {
	calls *sync.Map

	cachedMtx   sync.RWMutex
	cached      *list.List
	cachedBytes int64

	ttl      time.Duration
	maxItems int
	maxBytes int64
	force    bool
	metrics  *PostingsForMatchersCacheMetrics

	// Signal whether there's already a call to expire() in progress, in order to avoid multiple goroutines
	// cleaning up expired entries at the same time (1 at a time is enough).
	expireInProgress *atomic.Bool

	// timeNow is the time.Now that can be replaced for testing purposes
	timeNow func() time.Time

	// postingsForMatchers can be replaced for testing purposes
	postingsForMatchers func(ctx context.Context, ix IndexPostingsReader, ms ...*labels.Matcher) (index.Postings, error)

	// onPromiseExecutionDoneBeforeHook is used for testing purposes. It allows to hook at the
	// beginning of onPromiseExecutionDone() execution.
	onPromiseExecutionDoneBeforeHook func()

	// evictHeadBeforeHook is used for testing purposes. It allows to hook before calls to evictHead().
	evictHeadBeforeHook func()

	// Preallocated for performance
	additionalAttributes []attribute.KeyValue
}

func (c *PostingsForMatchersCache) PostingsForMatchers(ctx context.Context, ix IndexPostingsReader, concurrent bool, ms ...*labels.Matcher) (index.Postings, error) {
	c.metrics.requests.Inc()

	span := trace.SpanFromContext(ctx)
	defer func(startTime time.Time) {
		span.AddEvent(
			"PostingsForMatchers returned",
			trace.WithAttributes(c.additionalAttributes...),
			trace.WithAttributes(attribute.Bool("concurrent", concurrent), attribute.Stringer("duration", time.Since(startTime))),
		)
	}(time.Now())

	if !concurrent && !c.force {
		c.metrics.skipsBecauseIneligible.Inc()
		span.AddEvent("cache not used", trace.WithAttributes(c.additionalAttributes...))

		p, err := c.postingsForMatchers(ctx, ix, ms...)
		if err != nil {
			span.SetStatus(codes.Error, "getting postings for matchers without cache failed")
			span.RecordError(err)
		}
		return p, err
	}

	span.AddEvent("using cache", trace.WithAttributes(c.additionalAttributes...))
	c.expire()
	p, err := c.postingsForMatchersPromise(ctx, ix, ms)(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "getting postings for matchers with cache failed")
		span.RecordError(err)
	}
	return p, err
}

type postingsForMatcherPromise struct {
	// Keep track of all callers contexts in order to cancel the execution context if all
	// callers contexts get canceled.
	callersCtxTracker *contextsTracker

	// The result of the promise is stored either in cloner or err (only of the two is valued).
	// Do not access these fields until the done channel is closed.
	done   chan struct{}
	cloner *index.PostingsCloner
	err    error

	// Keep track of the time this promise completed evaluation.
	// Do not access this field until the done channel is closed.
	evaluationCompletedAt time.Time
}

func (p *postingsForMatcherPromise) result(ctx context.Context) (index.Postings, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("interrupting wait on postingsForMatchers promise due to context error: %w", ctx.Err())
	case <-p.done:
		// Checking context error is necessary for deterministic tests,
		// as channel selection order is random
		if ctx.Err() != nil {
			return nil, fmt.Errorf("completed postingsForMatchers promise, but context has error: %w", ctx.Err())
		}
		if p.err != nil {
			return nil, fmt.Errorf("postingsForMatchers promise completed with error: %w", p.err)
		}

		trace.SpanFromContext(ctx).AddEvent("completed postingsForMatchers promise", trace.WithAttributes(
			// Do not format the timestamp to introduce a performance regression.
			attribute.Int64("evaluation completed at (epoch seconds)", p.evaluationCompletedAt.Unix()),
			// We don't include the block ID because propagating it from the caller would increase the size of the promise.
			// With a bigger promise we can fit fewer promises in the cache, so the cache will be less effective.
			attribute.String("block", "unknown"),
		))

		return p.cloner.Clone(), nil
	}
}

func (c *PostingsForMatchersCache) postingsForMatchersPromise(ctx context.Context, ix IndexPostingsReader, ms []*labels.Matcher) func(context.Context) (index.Postings, error) {
	span := trace.SpanFromContext(ctx)

	promiseCallersCtxTracker, promiseExecCtx := newContextsTracker()
	promise := &postingsForMatcherPromise{
		done:              make(chan struct{}),
		callersCtxTracker: promiseCallersCtxTracker,
	}

	// Add the caller context to the ones tracked by the new promise.
	//
	// It's important to do it here so that if the promise will be stored, then the caller's context is already
	// tracked. Otherwise, if the promise will not be stored (because there's another in-flight promise for the
	// same label matchers) then it's not a problem, and resources will be released.
	//
	// Skipping the error checking because it can't happen here.
	_ = promise.callersCtxTracker.add(ctx)

	key := matchersKey(ms)

	if oldPromiseValue, loaded := c.calls.LoadOrStore(key, promise); loaded {
		// The new promise hasn't been stored because there's already an in-flight promise
		// for the same label matchers. We should just wait for it.

		// Release the resources created by the new promise, that will not be used.
		close(promise.done)
		promise.callersCtxTracker.close()

		oldPromise := oldPromiseValue.(*postingsForMatcherPromise)

		// Check if the promise already completed the execution and its TTL has not expired yet.
		// If the TTL has expired, we don't want to return it, so we just recompute the postings
		// on-the-fly, bypassing the cache logic. It's less performant, but more accurate, because
		// avoids returning stale data.
		if c.ttl > 0 {
			select {
			case <-oldPromise.done:
				if c.timeNow().Sub(oldPromise.evaluationCompletedAt) >= c.ttl {
					// The cached promise already expired, but it has not been evicted.
					span.AddEvent("skipping cached postingsForMatchers promise because its TTL already expired",
						trace.WithAttributes(
							attribute.Stringer("cached promise evaluation completed at", oldPromise.evaluationCompletedAt),
							attribute.String("cache_key", key),
						),
						trace.WithAttributes(c.additionalAttributes...),
					)
					c.metrics.skipsBecauseStale.Inc()

					return func(ctx context.Context) (index.Postings, error) {
						return c.postingsForMatchers(ctx, ix, ms...)
					}
				}

			default:
				// The evaluation is still in-flight. We wait for it.
			}
		}

		// Add the caller context to the ones tracked by the old promise (currently in-flight).
		if err := oldPromise.callersCtxTracker.add(ctx); err != nil && errors.Is(err, errContextsTrackerCanceled{}) {
			// We've hit a race condition happening when the "loaded" promise execution was just canceled,
			// but it hasn't been removed from map of calls yet, so the old promise was loaded anyway.
			//
			// We expect this race condition to be infrequent. In this case we simply skip the cache and
			// pass through the execution to the underlying postingsForMatchers().
			c.metrics.skipsBecauseCanceled.Inc()
			span.AddEvent(
				"looked up in-flight postingsForMatchers promise, but the promise was just canceled due to a race condition: skipping the cache",
				trace.WithAttributes(attribute.String("cache_key", key)),
				trace.WithAttributes(c.additionalAttributes...),
			)

			return func(ctx context.Context) (index.Postings, error) {
				return c.postingsForMatchers(ctx, ix, ms...)
			}
		}

		c.metrics.hits.Inc()
		span.AddEvent("waiting cached postingsForMatchers promise",
			trace.WithAttributes(attribute.String("cache_key", key)),
			trace.WithAttributes(c.additionalAttributes...),
		)

		return oldPromise.result
	}

	c.metrics.misses.Inc()
	span.AddEvent("no postingsForMatchers promise in cache, executing query", trace.WithAttributes(attribute.String("cache_key", key)), trace.WithAttributes(c.additionalAttributes...))

	// promise was stored, close its channel after fulfilment
	defer close(promise.done)

	// The execution context will be canceled only once all callers contexts will be canceled. This way we:
	// 1. Do not cancel postingsForMatchers() the input ctx is cancelled, but another goroutine is waiting
	//    for the promise result.
	// 2. Cancel postingsForMatchers() once all callers contexts have been canceled, so that we don't waist
	//    resources computing postingsForMatchers() is all requests have been canceled (this is particularly
	//    important if the postingsForMatchers() is very slow due to expensive regexp matchers).
	if postings, err := c.postingsForMatchers(promiseExecCtx, ix, ms...); err != nil {
		promise.err = err
	} else {
		promise.cloner = index.NewPostingsCloner(postings)
	}

	// Keep track of when the evaluation completed.
	promise.evaluationCompletedAt = c.timeNow()

	// The execution terminated (or has been canceled). We have to close the tracker to release resources.
	// It's important to close it before computing the promise size, so that the actual size is smaller.
	promise.callersCtxTracker.close()

	sizeBytes := int64(len(key) + size.Of(promise))

	c.onPromiseExecutionDone(ctx, key, promise.evaluationCompletedAt, sizeBytes, promise.err)
	return promise.result
}

type postingsForMatchersCachedCall struct {
	key string
	ts  time.Time

	// Size of the cached entry, in bytes.
	sizeBytes int64
}

func (c *PostingsForMatchersCache) expire() {
	if c.ttl <= 0 {
		return
	}

	// Ensure there's no other cleanup in progress. It's not a technical issue if there are two ongoing cleanups,
	// but it's a waste of resources and it adds extra pressure to the mutex. One cleanup at a time is enough.
	if !c.expireInProgress.CompareAndSwap(false, true) {
		return
	}

	defer c.expireInProgress.Store(false)

	c.cachedMtx.RLock()
	if !c.shouldEvictHead(c.timeNow()) {
		c.cachedMtx.RUnlock()
		return
	}
	c.cachedMtx.RUnlock()

	// Call the registered hook, if any. It's used only for testing purposes.
	if c.evictHeadBeforeHook != nil {
		c.evictHeadBeforeHook()
	}

	var evictionReasons [evictionReasonsLength]int

	// Evict the head taking an exclusive lock.
	{
		c.cachedMtx.Lock()

		now := c.timeNow()
		for c.shouldEvictHead(now) {
			reason := c.evictHead(now)
			evictionReasons[reason]++
		}

		c.cachedMtx.Unlock()
	}

	// Keep track of the reason why items where evicted.
	c.metrics.evictionsBecauseTTL.Add(float64(evictionReasons[evictionReasonTTL]))
	c.metrics.evictionsBecauseMaxBytes.Add(float64(evictionReasons[evictionReasonMaxBytes]))
	c.metrics.evictionsBecauseMaxItems.Add(float64(evictionReasons[evictionReasonMaxItems]))
	c.metrics.evictionsBecauseUnknown.Add(float64(evictionReasons[evictionReasonUnknown]))
}

// shouldEvictHead returns true if cache head should be evicted, either because it's too old,
// or because the cache has too many elements
// should be called while read lock is held on cachedMtx.
func (c *PostingsForMatchersCache) shouldEvictHead(now time.Time) bool {
	// The cache should be evicted for sure if the max size (either items or bytes) is reached.
	if c.cached.Len() > c.maxItems || c.cachedBytes > c.maxBytes {
		return true
	}

	h := c.cached.Front()
	if h == nil {
		return false
	}
	ts := h.Value.(*postingsForMatchersCachedCall).ts
	return now.Sub(ts) >= c.ttl
}

func (c *PostingsForMatchersCache) evictHead(now time.Time) (reason int) {
	front := c.cached.Front()
	oldest := front.Value.(*postingsForMatchersCachedCall)

	// Detect the reason why we're evicting it.
	//
	// The checks order is:
	// 1. TTL: if an item is expired, it should be tracked as such even if the cache was full.
	// 2. Max bytes: "max items" is deprecated, and we expect to set it to a high value because
	//    we want to primarily limit by bytes size.
	// 3. Max items: the last one.
	switch {
	case now.Sub(oldest.ts) >= c.ttl:
		reason = evictionReasonTTL
	case c.cachedBytes > c.maxBytes:
		reason = evictionReasonMaxBytes
	case c.cached.Len() > c.maxItems:
		reason = evictionReasonMaxItems
	default:
		// This should never happen, but we track it to detect unexpected behaviours.
		reason = evictionReasonUnknown
	}

	c.calls.Delete(oldest.key)
	c.cached.Remove(front)
	c.cachedBytes -= oldest.sizeBytes

	return
}

// onPromiseExecutionDone must be called once the execution of PostingsForMatchers promise has done.
// The input err contains details about any error that could have occurred when executing it.
// The input ts is the function call time.
func (c *PostingsForMatchersCache) onPromiseExecutionDone(ctx context.Context, key string, completedAt time.Time, sizeBytes int64, err error) {
	span := trace.SpanFromContext(ctx)

	// Call the registered hook, if any. It's used only for testing purposes.
	if c.onPromiseExecutionDoneBeforeHook != nil {
		c.onPromiseExecutionDoneBeforeHook()
	}

	// Do not cache if cache is disabled.
	if c.ttl <= 0 {
		span.AddEvent("not caching promise result because configured TTL is <= 0", trace.WithAttributes(c.additionalAttributes...))
		c.calls.Delete(key)
		return
	}

	// Do not cache if the promise execution was canceled (it gets cancelled once all the callers contexts have
	// been canceled).
	if errors.Is(err, context.Canceled) {
		span.AddEvent("not caching promise result because execution has been canceled", trace.WithAttributes(c.additionalAttributes...))
		c.calls.Delete(key)
		return
	}

	// Cache the promise.
	var lastCachedBytes int64
	{
		c.cachedMtx.Lock()

		c.cached.PushBack(&postingsForMatchersCachedCall{
			key:       key,
			ts:        completedAt,
			sizeBytes: sizeBytes,
		})
		c.cachedBytes += sizeBytes
		lastCachedBytes = c.cachedBytes

		c.cachedMtx.Unlock()
	}

	span.AddEvent("added cached value to expiry queue",
		trace.WithAttributes(
			attribute.Stringer("evaluation completed at", completedAt),
			attribute.Int64("size in bytes", sizeBytes),
			attribute.Int64("cached bytes", lastCachedBytes),
		),
		trace.WithAttributes(c.additionalAttributes...),
	)
}

// matchersKey provides a unique string key for the given matchers slice.
// NOTE: different orders of matchers will produce different keys,
// but it's unlikely that we'll receive same matchers in different orders at the same time.
func matchersKey(ms []*labels.Matcher) string {
	const (
		typeLen = 2
		sepLen  = 1
	)
	var size int
	for _, m := range ms {
		size += len(m.Name) + len(m.Value) + typeLen + sepLen
	}
	sb := strings.Builder{}
	sb.Grow(size)
	for _, m := range ms {
		sb.WriteString(m.Name)
		sb.WriteString(m.Type.String())
		sb.WriteString(m.Value)
		sb.WriteByte(0)
	}
	key := sb.String()
	return key
}

// indexReaderWithPostingsForMatchers adapts an index.Reader to be an IndexReader by adding the PostingsForMatchers method.
type indexReaderWithPostingsForMatchers struct {
	*index.Reader
	pfmc *PostingsForMatchersCache
}

func (ir indexReaderWithPostingsForMatchers) PostingsForMatchers(ctx context.Context, concurrent bool, ms ...*labels.Matcher) (index.Postings, error) {
	return ir.pfmc.PostingsForMatchers(ctx, ir, concurrent, ms...)
}

var _ IndexReader = indexReaderWithPostingsForMatchers{}

// errContextsTrackerClosed is the reason to identify contextsTracker has been explicitly closed by calling close().
//
// This error is a struct instead of a globally generic error so that postingsForMatcherPromise computed size is smaller
// (this error is referenced by contextsTracker, which is referenced by postingsForMatcherPromise).
type errContextsTrackerClosed struct{}

func (e errContextsTrackerClosed) Error() string {
	return "contexts tracker is closed"
}

// errContextsTrackerCanceled is the reason to identify contextsTracker has been automatically closed because
// all tracked contexts have been canceled.
//
// This error is a struct instead of a globally generic error so that postingsForMatcherPromise computed size is smaller
// (this error is referenced by contextsTracker, which is referenced by postingsForMatcherPromise).
type errContextsTrackerCanceled struct{}

func (e errContextsTrackerCanceled) Error() string {
	return "contexts tracker has been canceled"
}

// contextsTracker is responsible to monitor multiple context.Context and provides an execution
// that gets canceled once all monitored context.Context have done.
type contextsTracker struct {
	cancelExecCtx context.CancelFunc

	mx               sync.Mutex
	closedWithReason error         // Track whether the tracker is closed and why. The tracker is not closed if this is nil.
	trackedCount     int           // Number of tracked contexts.
	trackedStopFuncs []func() bool // The stop watching functions for all tracked contexts.
}

func newContextsTracker() (*contextsTracker, context.Context) {
	t := &contextsTracker{}

	// Create a new execution context that will be canceled only once all tracked contexts have done.
	var execCtx context.Context
	execCtx, t.cancelExecCtx = context.WithCancel(context.Background())

	return t, execCtx
}

// add the input ctx to the group of monitored context.Context.
// Returns false if the input context couldn't be added to the tracker because the tracker is already closed.
func (t *contextsTracker) add(ctx context.Context) error {
	t.mx.Lock()
	defer t.mx.Unlock()

	// Check if we've already done.
	if t.closedWithReason != nil {
		return t.closedWithReason
	}

	// Register a function that will be called once the tracked context has done.
	t.trackedCount++
	t.trackedStopFuncs = append(t.trackedStopFuncs, context.AfterFunc(ctx, t.onTrackedContextDone))

	return nil
}

// close the tracker. When the tracker is closed, the execution context is canceled
// and resources releases.
//
// This function must be called once done to not leak resources.
func (t *contextsTracker) close() {
	t.mx.Lock()
	defer t.mx.Unlock()

	t.unsafeClose(errContextsTrackerClosed{})
}

// unsafeClose must be called with the t.mx lock hold.
func (t *contextsTracker) unsafeClose(reason error) {
	if t.closedWithReason != nil {
		return
	}

	t.cancelExecCtx()

	// Stop watching the tracked contexts. It's safe to call the stop function on a context
	// for which was already done.
	for _, fn := range t.trackedStopFuncs {
		fn()
	}

	t.trackedCount = 0
	t.trackedStopFuncs = nil
	t.closedWithReason = reason
}

func (t *contextsTracker) onTrackedContextDone() {
	t.mx.Lock()
	defer t.mx.Unlock()

	t.trackedCount--

	// If this was the last context to be tracked, we can close the tracker and cancel the execution context.
	if t.trackedCount == 0 {
		t.unsafeClose(errContextsTrackerCanceled{})
	}
}

func (t *contextsTracker) trackedContextsCount() int {
	t.mx.Lock()
	defer t.mx.Unlock()

	return t.trackedCount
}

type PostingsForMatchersCacheMetrics struct {
	requests                 prometheus.Counter
	hits                     prometheus.Counter
	misses                   prometheus.Counter
	skipsBecauseIneligible   prometheus.Counter
	skipsBecauseStale        prometheus.Counter
	skipsBecauseCanceled     prometheus.Counter
	evictionsBecauseTTL      prometheus.Counter
	evictionsBecauseMaxBytes prometheus.Counter
	evictionsBecauseMaxItems prometheus.Counter
	evictionsBecauseUnknown  prometheus.Counter
}

func NewPostingsForMatchersCacheMetrics(reg prometheus.Registerer) *PostingsForMatchersCacheMetrics {
	const (
		skipsMetric = "postings_for_matchers_cache_skips_total"
		skipsHelp   = "Total number of requests to the PostingsForMatchers cache that have been skipped the cache. The subsequent result is not cached."

		evictionsMetric = "postings_for_matchers_cache_evictions_total"
		evictionsHelp   = "Total number of evictions from the PostingsForMatchers cache."
	)

	return &PostingsForMatchersCacheMetrics{
		requests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "postings_for_matchers_cache_requests_total",
			Help: "Total number of requests to the PostingsForMatchers cache.",
		}),
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "postings_for_matchers_cache_hits_total",
			Help: "Total number of postings lists returned from the PostingsForMatchers cache.",
		}),
		misses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "postings_for_matchers_cache_misses_total",
			Help: "Total number of requests to the PostingsForMatchers cache for which there is no valid cached entry. The subsequent result is cached.",
		}),
		skipsBecauseIneligible: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        skipsMetric,
			Help:        skipsHelp,
			ConstLabels: map[string]string{"reason": "ineligible"},
		}),
		skipsBecauseStale: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        skipsMetric,
			Help:        skipsHelp,
			ConstLabels: map[string]string{"reason": "stale-cached-entry"},
		}),
		skipsBecauseCanceled: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        skipsMetric,
			Help:        skipsHelp,
			ConstLabels: map[string]string{"reason": "canceled-cached-entry"},
		}),
		evictionsBecauseTTL: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        evictionsMetric,
			Help:        evictionsHelp,
			ConstLabels: map[string]string{"reason": "ttl-expired"},
		}),
		evictionsBecauseMaxBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        evictionsMetric,
			Help:        evictionsHelp,
			ConstLabels: map[string]string{"reason": "max-bytes-reached"},
		}),
		evictionsBecauseMaxItems: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        evictionsMetric,
			Help:        evictionsHelp,
			ConstLabels: map[string]string{"reason": "max-items-reached"},
		}),
		evictionsBecauseUnknown: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        evictionsMetric,
			Help:        evictionsHelp,
			ConstLabels: map[string]string{"reason": "unknown"},
		}),
	}
}
