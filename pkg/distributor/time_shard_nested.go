package distributor

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// nestedTimeShard is one bucket's worth of a stream: the entries whose timestamps fall in
// [start, end), and the total length of their lines, which rate sharding takes as the push size.
//
// The trailing shard, holding entries too recent to bucket, has a zero start and end. It keeps the
// stream's own name, where a bucketed shard is named after its window.
type nestedTimeShard struct {
	start, end    time.Time
	stream        logproto.InternalStreamAdapter
	linesTotalLen int
}

// recent reports whether this is the trailing shard rather than a bucket. A bucket's start is a
// truncated entry timestamp, and entry timestamps come off the wire through time.Unix, so the zero
// time is the trailing shard's alone.
func (s *nestedTimeShard) recent() bool { return s.start.IsZero() }

// timeShardNested splits a nested stream into buckets of shardLen by entry timestamp, with entries
// newer than ignoreLogsFrom left in a trailing shard of their own. It reports false when there is
// nothing old enough to bucket, in which case the caller uses the stream as it stands.
//
// Which bucket an entry belongs to depends only on its own timestamp, so nothing is sorted: the flat
// path's sort is there to make its buckets contiguous sub-slices of one array, which this does not
// need. Groups are walked one at a time, which is what lets a bucket open each group once.
//
// Entries therefore keep the order they arrived in, where the flat path leaves each of its shards in
// timestamp order. Neither is sorted as the ingester sees it, since a bucket interleaves groups
// either way, and a bucket spans MaxChunkAge/2, which is exactly the window within which the
// ingester accepts entries in any order.
func timeShardNested(stream *logproto.InternalStreamAdapter, shardLen time.Duration, ignoreLogsFrom time.Time) ([]nestedTimeShard, bool) {
	if nestedEntryCount(stream) == 0 {
		return nil, false
	}

	// Nothing to do if every entry is recent, which is the common case and the same shortcut the
	// flat path takes.
	if oldestEntry(stream).After(ignoreLogsFrom) {
		return nil, false
	}

	// Buckets are keyed by the nanosecond their window opens, which is how Loki carries a log
	// timestamp everywhere else. Whole seconds would merge two windows of a sub-second shardLen.
	buckets := map[int64]*timeBucket{}
	var trailing *timeBucket

	bucket := func(m map[int64]*timeBucket, start int64) *timeBucket {
		if m[start] == nil {
			m[start] = &timeBucket{
				stream:       logproto.InternalStreamAdapter{Labels: stream.Labels, Hash: stream.Hash},
				lastResource: -1,
			}
		}
		return m[start]
	}

	// byBucket buffers all entries of a single group by their time bucket.
	byBucket := map[int64][]push.Entry{}
	for resourceIdx := range stream.ResourceLogs {
		resource := &stream.ResourceLogs[resourceIdx]

		for scopeIdx := range resource.ScopeLogs {
			scope := &resource.ScopeLogs[scopeIdx]

			// clear the entries buffered in the bucket
			clear(byBucket)
			var recent []push.Entry
			for entryIdx := range scope.Entries {
				entry := &scope.Entries[entryIdx]
				if !entry.Timestamp.Before(ignoreLogsFrom) {
					recent = append(recent, *entry)
					continue
				}
				start := entry.Timestamp.Truncate(shardLen).UnixNano()
				byBucket[start] = append(byBucket[start], *entry)
			}

			for start, entries := range byBucket {
				bucket(buckets, start).add(resourceIdx, resource, scope, entries)
			}
			if len(recent) > 0 {
				if trailing == nil {
					trailing = &timeBucket{
						stream:       logproto.InternalStreamAdapter{Labels: stream.Labels, Hash: stream.Hash},
						lastResource: -1,
					}
				}
				trailing.add(resourceIdx, resource, scope, recent)
			}
		}
	}

	starts := make([]int64, 0, len(buckets))
	for start := range buckets {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })

	// Oldest bucket first, with the trailing shard last, as the flat path returns them.
	shards := make([]nestedTimeShard, 0, len(starts)+1)
	for _, start := range starts {
		at := time.Unix(0, start).UTC()
		shards = append(shards, nestedTimeShard{
			start:         at,
			end:           at.Add(shardLen),
			stream:        buckets[start].stream,
			linesTotalLen: buckets[start].linesTotalLen,
		})
	}
	if trailing != nil {
		shards = append(shards, nestedTimeShard{
			stream:        trailing.stream,
			linesTotalLen: trailing.linesTotalLen,
		})
	}
	return shards, true
}

// timeBucket accumulates the entries of one window. Only the resource it is filling is tracked:
// groups arrive one at a time, so a bucket takes a scope's entries in a single handful and never
// returns to that scope, but two scopes of one resource do both reach it and share its resource.
type timeBucket struct {
	stream        logproto.InternalStreamAdapter
	lastResource  int
	linesTotalLen int
}

func (b *timeBucket) add(resourceIdx int, resource *logproto.ResourceLogs, scope *logproto.ScopeLogs, entries []push.Entry) {
	if resourceIdx != b.lastResource {
		b.stream.ResourceLogs = append(b.stream.ResourceLogs, logproto.ResourceLogs{Attrs: resource.Attrs})
		b.lastResource = resourceIdx
	}

	last := &b.stream.ResourceLogs[len(b.stream.ResourceLogs)-1]
	last.ScopeLogs = append(last.ScopeLogs, logproto.ScopeLogs{Attrs: scope.Attrs, Entries: entries})

	for i := range entries {
		b.linesTotalLen += len(entries[i].Line)
	}
}

// oldestEntry is the earliest timestamp the stream holds. The caller has already established that
// it holds one.
func oldestEntry(stream *logproto.InternalStreamAdapter) time.Time {
	var oldest time.Time
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			for _, entry := range stream.ResourceLogs[i].ScopeLogs[j].Entries {
				if oldest.IsZero() || entry.Timestamp.Before(oldest) {
					oldest = entry.Timestamp
				}
			}
		}
	}
	return oldest
}
