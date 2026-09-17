package distributor

import (
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// nestedTimeShard is one bucket's worth of a stream: the entries whose timestamps fall in
// [start, end), and the total length of their lines, which rate sharding takes as the push size.
//
// The trailing shard, holding entries too recent to bucket, has a zero start and end. It keeps the
// stream's own name, where a bucketed shard carries the window in its __time_shard__ label.
//
// A shard holds its entries as subslices of the stream it came from, so a caller that rewrites an
// entry rewrites it for both.
type nestedTimeShard struct {
	start, end    time.Time
	stream        logproto.InternalStreamAdapter
	linesTotalLen int
}

// recent reports whether this is the trailing shard rather than a bucket. A bucket's start is a
// truncated entry timestamp, and entry timestamps come off the wire through time.Unix, so the zero
// time is the trailing shard's alone.
func (s *nestedTimeShard) recent() bool { return s.start.IsZero() }

// timeShardNested splits a nested stream into buckets of shardLen by entry timestamp, each named
// after its window in the __time_shard__ label built on lbls. Entries newer than ignoreLogsFrom are
// left in a trailing shard, unnamed; false means nothing was old enough to bucket at all.
//
// It sorts each group's entries in place, so a bucket takes its share of a group as one subslice
// instead of copying entries. A bucket therefore holds each group in timestamp order and
// interleaves the groups, which the ingester accepts: a bucket spans MaxChunkAge/2, its window for
// unordered writes.
func timeShardNested(stream *logproto.InternalStreamAdapter, lbls labels.Labels, shardLen time.Duration, ignoreLogsFrom time.Time) ([]nestedTimeShard, bool) {
	if nestedEntryCount(stream) == 0 {
		return nil, false
	}

	// Nothing to do if every entry is recent, which is the common case.
	if oldestEntry(stream).After(ignoreLogsFrom) {
		return nil, false
	}

	// Buckets are keyed by the nanosecond their window opens, which is how Loki carries a log
	// timestamp everywhere else. Whole seconds would merge two windows of a sub-second shardLen.
	buckets := map[int64]*timeBucket{}
	var trailing *timeBucket

	bucket := func(start int64) *timeBucket {
		if buckets[start] == nil {
			buckets[start] = &timeBucket{
				stream:       logproto.InternalStreamAdapter{Labels: stream.Labels, Hash: stream.Hash},
				lastResource: -1,
			}
		}
		return buckets[start]
	}

	for resourceIdx := range stream.ResourceLogs {
		resource := &stream.ResourceLogs[resourceIdx]

		for scopeIdx := range resource.ScopeLogs {
			scope := &resource.ScopeLogs[scopeIdx]
			entries := scope.Entries
			slices.SortStableFunc(entries, func(a, b logproto.Entry) int { return a.Timestamp.Compare(b.Timestamp) })

			// Runs of entries, each falling in one window. Every subslice is capped at its own
			// length, so appending to one bucket's entries reallocates rather than overwriting
			// the run the next bucket holds.
			for i := 0; i < len(entries); {
				if !entries[i].Timestamp.Before(ignoreLogsFrom) {
					// Sorted, so everything left is too recent to bucket.
					if trailing == nil {
						trailing = &timeBucket{
							stream:       logproto.InternalStreamAdapter{Labels: stream.Labels, Hash: stream.Hash},
							lastResource: -1,
						}
					}
					trailing.add(resourceIdx, resource, scope, entries[i:len(entries):len(entries)])
					break
				}

				start := entries[i].Timestamp.Truncate(shardLen)
				// A window reaching past the cutoff ends there, the rest of it being too recent.
				cutoff := start.Add(shardLen)
				if cutoff.After(ignoreLogsFrom) {
					cutoff = ignoreLogsFrom
				}

				j := i + 1
				for j < len(entries) && entries[j].Timestamp.Before(cutoff) {
					j++
				}
				bucket(start.UnixNano()).add(resourceIdx, resource, scope, entries[i:j:j])
				i = j
			}
		}
	}

	starts := make([]int64, 0, len(buckets))
	for start := range buckets {
		starts = append(starts, start)
	}
	slices.Sort(starts)

	// Oldest bucket first, with the trailing shard last.
	labelBuilder := labels.NewBuilder(lbls)
	shards := make([]nestedTimeShard, 0, len(starts)+1)
	for _, start := range starts {
		at := time.Unix(0, start).UTC()
		end := at.Add(shardLen)

		shardLbls := labelBuilder.Set(timeShardLabel, fmt.Sprintf("%d_%d", at.Unix(), end.Unix())).Labels()
		shard := buckets[start].stream
		shard.Labels = shardLbls.String()
		shard.Hash = labels.StableHash(shardLbls)

		shards = append(shards, nestedTimeShard{
			start:         at,
			end:           end,
			stream:        shard,
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

func (b *timeBucket) add(resourceIdx int, resource *logproto.ResourceLogs, scope *logproto.ScopeLogs, entries []logproto.Entry) {
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
