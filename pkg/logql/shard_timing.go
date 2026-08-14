package logql

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// shardLabel is the label the distributor adds to streams split by rate-based
// stream sharding. It is redefined here because its canonical home
// (pkg/ingester ShardLbName) imports pkg/logql and cannot be imported back.
const shardLabel = "__stream_shard__"

// shardStreamsTopK bounds how many logical streams each subquery reports in its
// stats (by descending time), keeping the stats payload and frontend merge
// bounded. The dominant (widest) stream is ~top-1 on every subquery, so the
// critical-path estimate survives the cap.
const shardStreamsTopK = 50

// shardLabelRE removes the __stream_shard__="N" token (and one adjacent
// separator) from a rendered labels string, yielding a key that is identical
// across all shards of the same logical stream. It matches the token whether it
// is first/middle (trailing ", ") or last (leading ", ").
var shardLabelRE = regexp.MustCompile(`__stream_shard__="[^"]*", |, __stream_shard__="[^"]*"`)

func stripShard(labels string) string {
	return shardLabelRE.ReplaceAllString(labels, "")
}

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

	// perStream accumulates, per logical stream (labels with __stream_shard__
	// removed), the time spent on its shards in THIS subquery and the set of
	// distinct shards seen. The frontend sums these across subqueries to get
	// T_s and maxes them to get M_s.
	perStream map[string]*perStreamAgg

	// Cache the verdict for the last seen labels string so consecutive
	// entries of the same stream skip the Contains check and the shard strip.
	lastLabels     string
	lastSharded    bool
	lastLogicalKey string
}

type perStreamAgg struct {
	durNanos int64
	shards   map[string]struct{}
}

type shardTimeTrackerCtxKey struct{}

func withShardTimeTracker(ctx context.Context) context.Context {
	return context.WithValue(ctx, shardTimeTrackerCtxKey{}, &shardTimeTracker{
		sharded:   make(map[string]struct{}),
		unsharded: make(map[string]struct{}),
		perStream: make(map[string]*perStreamAgg),
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
			t.lastLogicalKey = stripShard(labels)
		} else {
			t.unsharded[labels] = struct{}{}
		}
	}
	if isSharded {
		t.shardedNanos += elapsed.Nanoseconds()
		agg := t.perStream[t.lastLogicalKey]
		if agg == nil {
			agg = &perStreamAgg{shards: make(map[string]struct{})}
			t.perStream[t.lastLogicalKey] = agg
		}
		agg.durNanos += elapsed.Nanoseconds()
		agg.shards[labels] = struct{}{}
	} else {
		t.unshardedNanos += elapsed.Nanoseconds()
	}
}

// perStreamTop returns the top-k logical streams by time spent in this subquery.
// For a single subquery sum == max (there is one value per stream); the frontend
// sums (→ T_s) and maxes (→ M_s) these across a parent query's subqueries.
func (t *shardTimeTracker) perStreamTop(k int) []stats.ShardedStream {
	if t == nil {
		return nil
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	out := make([]stats.ShardedStream, 0, len(t.perStream))
	for key, agg := range t.perStream {
		out = append(out, stats.ShardedStream{
			Stream:           key,
			SumDurationNanos: agg.durNanos,
			MaxDurationNanos: agg.durNanos,
			Shards:           int64(len(agg.shards)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SumDurationNanos > out[j].SumDurationNanos })
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
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
