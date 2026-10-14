package distributor

import (
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

// nestedTimeShard holds entries in [start, end) and their expanded size for rate sharding.
// A recent shard has zero start/end and retains the source stream's labels.
// Entry slices share storage with the source stream.
type nestedTimeShard struct {
	start, end time.Time
	stream     logproto.InternalStreamAdapter
	// Parsed labels reused for rate sharding.
	lbls         labels.Labels
	expandedSize int
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
func timeShardNested(stream logproto.InternalStreamAdapter, lbls labels.Labels, shardLen time.Duration, ignoreLogsFrom time.Time) ([]nestedTimeShard, bool) {
	if stream.EntryCount() == 0 {
		return nil, false
	}

	// Preserve the flat time sharder's ordering by sorting before the recent-entry check.
	var oldest *logproto.Entry
	stream.EachGroup(func(_, _ []logproto.LabelAdapter, entries []logproto.Entry) {
		slices.SortStableFunc(entries, func(a, b logproto.Entry) int { return a.Timestamp.Compare(b.Timestamp) })
		if len(entries) > 0 && (oldest == nil || entries[0].Timestamp.Before(oldest.Timestamp)) {
			oldest = &entries[0]
		}
	})

	if oldest.Timestamp.After(ignoreLogsFrom) {
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
		resourceSize := util.StructuredMetadataSize(resource.Attrs)

		for scopeIdx := range resource.ScopeLogs {
			scope := &resource.ScopeLogs[scopeIdx]
			sharedSize := resourceSize + util.StructuredMetadataSize(scope.Attrs)
			entries := scope.Entries

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
					expandedSize := (len(entries) - i) * sharedSize
					for j := i; j < len(entries); j++ {
						expandedSize += util.EntryTotalSize(&entries[j])
					}
					trailing.add(resourceIdx, resource, scope, entries[i:len(entries):len(entries)], expandedSize)
					break
				}

				start := entries[i].Timestamp.Truncate(shardLen)
				// A window reaching past the cutoff ends there, the rest of it being too recent.
				cutoff := start.Add(shardLen)
				if cutoff.After(ignoreLogsFrom) {
					cutoff = ignoreLogsFrom
				}

				expandedSize := util.EntryTotalSize(&entries[i])
				j := i + 1
				for j < len(entries) && entries[j].Timestamp.Before(cutoff) {
					expandedSize += util.EntryTotalSize(&entries[j])
					j++
				}
				expandedSize += (j - i) * sharedSize
				bucket(start.UnixNano()).add(resourceIdx, resource, scope, entries[i:j:j], expandedSize)
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
			start:        at,
			end:          end,
			stream:       shard,
			lbls:         shardLbls,
			expandedSize: buckets[start].expandedSize,
		})
	}
	if trailing != nil {
		shards = append(shards, nestedTimeShard{
			stream:       trailing.stream,
			lbls:         lbls,
			expandedSize: trailing.expandedSize,
		})
	}
	return shards, true
}

// timeBucket accumulates the entries of one window. Only the resource it is filling is tracked:
// groups arrive one at a time, so a bucket takes a scope's entries in a single handful and never
// returns to that scope, but two scopes of one resource do both reach it and share its resource.
type timeBucket struct {
	stream       logproto.InternalStreamAdapter
	lastResource int
	expandedSize int
}

func (b *timeBucket) add(resourceIdx int, resource *logproto.ResourceLogs, scope *logproto.ScopeLogs, entries []logproto.Entry, expandedSize int) {
	if resourceIdx != b.lastResource {
		b.stream.ResourceLogs = append(b.stream.ResourceLogs, logproto.ResourceLogs{Attrs: resource.Attrs})
		b.lastResource = resourceIdx
	}

	last := &b.stream.ResourceLogs[len(b.stream.ResourceLogs)-1]
	last.ScopeLogs = append(last.ScopeLogs, logproto.ScopeLogs{Attrs: scope.Attrs, Entries: entries})

	b.expandedSize += expandedSize
}
