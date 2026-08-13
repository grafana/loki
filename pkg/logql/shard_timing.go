package logql

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
)

// shardLabel is the label the distributor adds to streams split by rate-based
// stream sharding. It is redefined here because its canonical home
// (pkg/ingester ShardLbName) imports pkg/logql and cannot be imported back.
const shardLabel = "__stream_shard__"

// shardTimeTracker is temporary instrumentation to measure the query-side
// effect of stream sharding: it accumulates, per query, the time spent
// pulling entries/samples that belong to streams carrying the
// __stream_shard__ label (vs. those that don't), plus distinct stream counts
// for each bucket. The totals are logged on the metrics.go line.
//
// Timing happens at the top-level iterator consumption loops (readStreams and
// the range-vector iterators). The pipeline is lazy, so decompression,
// filtering and merging costs all surface at those Next() calls. Merge time
// is attributed to whichever stream's entry surfaced — an accepted
// approximation.
type shardTimeTracker struct {
	mtx sync.Mutex

	shardedNanos   int64
	unshardedNanos int64
	sharded        map[string]struct{}
	unsharded      map[string]struct{}

	// Cache the verdict for the last seen labels string so consecutive
	// entries of the same stream skip the Contains check.
	lastLabels  string
	lastSharded bool
}

type shardTimeTrackerCtxKey struct{}

func withShardTimeTracker(ctx context.Context) context.Context {
	return context.WithValue(ctx, shardTimeTrackerCtxKey{}, &shardTimeTracker{
		sharded:   make(map[string]struct{}),
		unsharded: make(map[string]struct{}),
	})
}

func shardTrackerFromContext(ctx context.Context) *shardTimeTracker {
	t, _ := ctx.Value(shardTimeTrackerCtxKey{}).(*shardTimeTracker)
	return t
}

// observe attributes elapsed to the sharded or unsharded bucket based on the
// stream's labels string. Nil-safe so call sites need no checks.
func (t *shardTimeTracker) observe(labels string, elapsed time.Duration) {
	if t == nil {
		return
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	isSharded := t.lastSharded
	if labels != t.lastLabels {
		isSharded = strings.Contains(labels, shardLabel)
		t.lastLabels = labels
		t.lastSharded = isSharded
		if isSharded {
			t.sharded[labels] = struct{}{}
		} else {
			t.unsharded[labels] = struct{}{}
		}
	}
	if isSharded {
		t.shardedNanos += elapsed.Nanoseconds()
	} else {
		t.unshardedNanos += elapsed.Nanoseconds()
	}
}

func (t *shardTimeTracker) snapshot() (shardedStreams, unshardedStreams int, shardedDuration, unshardedDuration time.Duration) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return len(t.sharded), len(t.unsharded), time.Duration(t.shardedNanos), time.Duration(t.unshardedNanos)
}

// trackedNextEntry advances it, attributing the time spent to the stream of
// the entry that surfaced. tr may be nil.
func trackedNextEntry(tr *shardTimeTracker, it iter.EntryIterator) bool {
	if tr == nil {
		return it.Next()
	}
	start := time.Now()
	ok := it.Next()
	if ok {
		tr.observe(it.Labels(), time.Since(start))
	}
	return ok
}

// trackedNextSample consumes the current sample of a peeking iterator,
// attributing the pull time to lbs (the labels returned by Peek). tr may be
// nil.
func trackedNextSample(tr *shardTimeTracker, it iter.PeekingSampleIterator, lbs string) {
	if tr == nil {
		_ = it.Next()
		return
	}
	start := time.Now()
	_ = it.Next()
	tr.observe(lbs, time.Since(start))
}
