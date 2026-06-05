package scheduler

import (
	"sync"

	"github.com/oklog/ulid/v2"
)

// manifestResources holds the streams and tasks belonging to a single
// registered manifest, guarded by its own mutex.
//
// Isolating each manifest's resources behind its own lock is what lets a large
// registration (or any per-task/per-stream hot-path operation) for one query
// proceed without serializing against unrelated queries. Because a manifest is
// a closed unit — every stream only references tasks within the same manifest —
// all of the cross-references the scheduler performs (a stream's receiver task,
// a task's source/sink streams, etc.) stay within a single manifestResources
// and are therefore covered by this one lock.
type manifestResources struct {
	id ulid.ULID

	mu      sync.RWMutex
	streams map[ulid.ULID]*stream
	tasks   map[ulid.ULID]*task
}

func newManifestResources(id ulid.ULID, streamCap, taskCap int) *manifestResources {
	return &manifestResources{
		id:      id,
		streams: make(map[ulid.ULID]*stream, streamCap),
		tasks:   make(map[ulid.ULID]*task, taskCap),
	}
}

// resourceIndexShards is the number of shards used by [resourceIndex]. Picked to
// spread the lock across enough buckets that a single large registration only
// briefly holds any one shard while other manifests' hot-path lookups proceed.
const resourceIndexShards = 256

// resourceIndex routes a task or stream ULID to the manifestResources that owns
// it. It is sharded by ULID so that a registration inserting tens of thousands
// of entries never holds a single lock for O(n) — each entry only briefly locks
// one shard — and concurrent hot-path lookups for other manifests almost always
// hit a different shard. Plain maps are used per shard to avoid the per-entry
// allocation overhead of sync.Map under the high write churn of registration.
type resourceIndex struct {
	shards [resourceIndexShards]resourceIndexShard
}

type resourceIndexShard struct {
	mu sync.RWMutex
	m  map[ulid.ULID]*manifestResources
}

func newResourceIndex() *resourceIndex {
	ri := &resourceIndex{}
	for i := range ri.shards {
		ri.shards[i].m = make(map[ulid.ULID]*manifestResources)
	}
	return ri
}

// shardFor selects a shard using the final ULID byte, which is part of the
// 80-bit random component and therefore uniformly distributed (the leading
// bytes are a timestamp and would cluster).
func (ri *resourceIndex) shardFor(id ulid.ULID) *resourceIndexShard {
	return &ri.shards[id[len(id)-1]]
}

func (ri *resourceIndex) load(id ulid.ULID) (*manifestResources, bool) {
	sh := ri.shardFor(id)
	sh.mu.RLock()
	mr, ok := sh.m[id]
	sh.mu.RUnlock()
	return mr, ok
}

// loadOrStore stores mr for id if no entry exists, returning ok=false. If a
// (possibly different) manifest already owns id, it leaves the index untouched
// and returns the existing value with ok=true.
func (ri *resourceIndex) loadOrStore(id ulid.ULID, mr *manifestResources) (*manifestResources, bool) {
	sh := ri.shardFor(id)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if existing, ok := sh.m[id]; ok {
		return existing, true
	}
	sh.m[id] = mr
	return mr, false
}

func (ri *resourceIndex) delete(id ulid.ULID) {
	sh := ri.shardFor(id)
	sh.mu.Lock()
	delete(sh.m, id)
	sh.mu.Unlock()
}

// manifestForTask returns the manifestResources that owns the given task ULID.
// Callers must take mr.mu before touching the returned manifest's maps.
func (s *Scheduler) manifestForTask(id ulid.ULID) (*manifestResources, bool) {
	return s.taskIndex.load(id)
}

// manifestForStream returns the manifestResources that owns the given stream
// ULID. Callers must take mr.mu before touching the returned manifest's maps.
func (s *Scheduler) manifestForStream(id ulid.ULID) (*manifestResources, bool) {
	return s.streamIndex.load(id)
}

// rangeManifests calls fn for every currently registered manifest. fn must take
// mr.mu itself if it needs to read the manifest's maps.
func (s *Scheduler) rangeManifests(fn func(mr *manifestResources)) {
	s.manifests.Range(func(_, v any) bool {
		fn(v.(*manifestResources))
		return true
	})
}
