package distributor

import (
	"strconv"
	"strings"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// shardNested divides a nested stream's entries across shards, each taking a contiguous run of
// every group it reaches into. Shards are named from lbls, which carries the placeholder
// labelTemplate leaves for the shard label, and numbered from startShard, wrapping at shards — the
// caller tracks where the stream's last push left off.
//
// A run is a subslice of the source rather than a copy, so a caller that rewrites an entry rewrites
// it for both. Contiguous runs are what make that possible, and what hold a group's attributes to
// R+S-1 resource and G+S-1 scope copies over S shards: repeated only where a boundary falls inside
// the group.
func shardNested(stream *logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int) []logproto.InternalStreamAdapter {
	total := nestedEntryCount(stream)
	if total == 0 || shards < 1 {
		return nil
	}

	// No more shards than there are entries to fill them.
	if shards > total {
		shards = total
	}

	// The remainder is spread over the leading shards rather than left to the last one.
	base, remainder := total/shards, total%shards
	shardQuota := func(shard int) int {
		if shard < remainder {
			return base + 1
		}
		return base
	}

	lblsStr := lbls.String()
	out := make([]logproto.InternalStreamAdapter, shards)
	for i := range out {
		out[i].Labels, out[i].Hash = shardIdentity(lbls, lblsStr, (startShard+i)%shards)
	}

	shard, placed := 0, 0
	for _, sourceResource := range stream.ResourceLogs {
		// The resource being filled in the shard being filled, nil until a run lands in it. That
		// is what keeps an empty one out of a shard, and what reopens it in the next shard when a
		// boundary falls inside it.
		var resource *logproto.ResourceLogs

		for _, sourceScope := range sourceResource.ScopeLogs {
			var scope *logproto.ScopeLogs

			// Entries to shard from the current scope.
			remaining := sourceScope.Entries

			for len(remaining) > 0 {
				if placed == shardQuota(shard) && shard+1 < shards {
					shard, placed = shard+1, 0
					resource, scope = nil, nil
				}

				remainingCount := len(remaining)
				// If it is not the last shard, limit the entries we take for the shard up to its remaining capacity.
				if shardCapacityLeft := shardQuota(shard) - placed; shard+1 < shards && remainingCount > shardCapacityLeft {
					remainingCount = shardCapacityLeft
				}

				if resource == nil {
					out[shard].ResourceLogs = append(out[shard].ResourceLogs,
						logproto.ResourceLogs{Attrs: sourceResource.Attrs})
					resource = &out[shard].ResourceLogs[len(out[shard].ResourceLogs)-1]
				}
				if scope == nil {
					resource.ScopeLogs = append(resource.ScopeLogs,
						logproto.ScopeLogs{Attrs: sourceScope.Attrs})
					scope = &resource.ScopeLogs[len(resource.ScopeLogs)-1]
				}

				// Capped at its own length, so appending to one shard's entries reallocates
				// rather than overwriting the entries the next shard is about to take.
				scope.Entries = remaining[:remainingCount:remainingCount]
				remaining = remaining[remainingCount:]
				placed += remainingCount
			}
		}
	}
	return out
}

// shardIdentity is the name and hash a shard takes from its number, with the placeholder that
// labelTemplate left in the stream's name replaced by it.
func shardIdentity(lbls labels.Labels, streamPattern string, shardNumber int) (string, uint64) {
	shardLabel := strconv.Itoa(shardNumber)

	builder := labels.NewBuilder(lbls)
	if lbls.Has(ingester.ShardLbName) {
		builder.Set(ingester.ShardLbName, shardLabel)
	}

	return strings.Replace(streamPattern, ingester.ShardLbPlaceholder, shardLabel, 1), labels.StableHash(builder.Labels())
}

// nestedEntryCount is the number of entries the stream holds, across every group.
func nestedEntryCount(stream *logproto.InternalStreamAdapter) int {
	n := 0
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			n += len(stream.ResourceLogs[i].ScopeLogs[j].Entries)
		}
	}
	return n
}
