package distributor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/grafana/loki/v3/pkg/util/topk"
)

const (
	// tenantBytesWindowSize is the size of the rolling window over which
	// per-tenant received bytes are summed.
	tenantBytesWindowSize = time.Minute

	// tenantBytesBucketSize is the granularity of the rolling window. The
	// window is divided into buckets of this size, and each bucket is reset
	// when it is re-used a window later.
	tenantBytesBucketSize = 15 * time.Second

	// tenantBytesNumBuckets is the number of buckets in the rolling window.
	tenantBytesNumBuckets = int(tenantBytesWindowSize / tenantBytesBucketSize)

	// topTenantsCount is the number of tenants, ordered by the number of bytes
	// received in the rolling window, that are rejected when the distributor is
	// close to its max inflight bytes.
	topTenantsCount = 3

	// topTenantsInflightRatio is the fraction of max inflight bytes at which
	// the top tenants start being rejected.
	topTenantsInflightRatio = 0.75

	// topTenantsRefreshPeriod is the minimum interval between recalculations of
	// the top tenants. Ranking is O(tenants), so it must not run on every push.
	topTenantsRefreshPeriod = time.Second

	// topTenantsErrorMsg is returned to tenants that are rejected because the
	// distributor is close to its max inflight bytes and they are one of the
	// top tenants by the number of bytes received.
	topTenantsErrorMsg = "The server is under high load and this tenant is one of the largest senders of data. Please retry later."
)

// A bytesBucket is one bucket of a tenant's rolling window.
type bytesBucket struct {
	// start is the start of the bucket in Unix nanoseconds. It is used to
	// detect when a bucket is being re-used and must be reset.
	start int64
	bytes int64
}

// A tenantBytesWindow keeps a rolling window of the number of bytes received
// per tenant, and the set of the top N tenants within that window.
//
// The window is a circular list of buckets. Rather than rotating the buckets on
// a ticker, a bucket is reset the first time it is written to in a new
// interval, which means no background goroutine is required.
//
// It is safe for concurrent use.
type tenantBytesWindow struct {
	// top is the current set of top tenants. It is replaced wholesale on each
	// refresh so isTopTenant does not need to take the mutex. It is never nil
	// once the window has been created.
	top atomic.Pointer[map[string]struct{}]

	mtx         sync.Mutex
	tenants     map[string]*[tenantBytesNumBuckets]bytesBucket
	lastRefresh time.Time
}

func newTenantBytesWindow() *tenantBytesWindow {
	w := tenantBytesWindow{
		tenants: make(map[string]*[tenantBytesNumBuckets]bytesBucket),
	}
	w.top.Store(&map[string]struct{}{})
	return &w
}

// observe records that size bytes were received for the tenant at now. It also
// refreshes the set of top tenants if it has not been refreshed recently.
func (w *tenantBytesWindow) observe(tenantID string, size int64, now time.Time) {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	buckets, ok := w.tenants[tenantID]
	if !ok {
		buckets = &[tenantBytesNumBuckets]bytesBucket{}
		w.tenants[tenantID] = buckets
	}

	// Buckets are a circular list, so the same index is re-used once per
	// window. If the bucket at this index is from an earlier interval then it
	// is outside the window and must be reset before it can be re-used.
	idx := bucketIndex(now)
	start := now.Truncate(tenantBytesBucketSize).UnixNano()
	if buckets[idx].start < start {
		buckets[idx].start = start
		buckets[idx].bytes = 0
	}
	buckets[idx].bytes += size

	if now.Sub(w.lastRefresh) >= topTenantsRefreshPeriod {
		w.refreshLocked(now)
	}
}

// isTopTenant returns true if the tenant is one of the top N tenants as of the
// last refresh.
func (w *tenantBytesWindow) isTopTenant(tenantID string) bool {
	_, ok := (*w.top.Load())[tenantID]
	return ok
}

// refreshLocked recalculates the set of top tenants and evicts tenants that
// have not received data within the window. w.mtx must be held.
func (w *tenantBytesWindow) refreshLocked(now time.Time) {
	w.lastRefresh = now

	// A limited min-heap keeps the greatest topTenantsCount elements.
	h := topk.Heap[tenantBytes]{
		Limit: topTenantsCount,
		Less:  func(a, b tenantBytes) bool { return a.bytes < b.bytes },
	}

	cutoff := now.Add(-tenantBytesWindowSize).UnixNano()
	for tenantID, buckets := range w.tenants {
		var total int64
		for _, bucket := range buckets {
			if bucket.start > cutoff {
				total += bucket.bytes
			}
		}
		if total == 0 {
			// The tenant has not received data within the window.
			delete(w.tenants, tenantID)
			continue
		}
		h.Push(tenantBytes{tenantID: tenantID, bytes: total})
	}

	top := make(map[string]struct{}, h.Len())
	for t := range h.Range() {
		top[t.tenantID] = struct{}{}
	}
	w.top.Store(&top)
}

// tenantBytes is the number of bytes received for a tenant within the window.
type tenantBytes struct {
	tenantID string
	bytes    int64
}

// bucketIndex returns the index of the bucket for t.
func bucketIndex(t time.Time) int {
	return int((t.UnixNano() / int64(tenantBytesBucketSize)) % int64(tenantBytesNumBuckets))
}
