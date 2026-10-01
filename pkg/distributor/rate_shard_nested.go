package distributor

import (
	"strconv"
	"strings"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// shardNested divides a nested stream's entries across shards, each taking a contiguous run of
// every group it reaches into. Shards are named from lbls, the stream's own labels, carrying the
// shard label set to a number taken from startShard onwards and wrapping at shards — the caller
// tracks where the stream's last push left off.
//
// A run is a subslice of the source rather than a copy, so a caller that rewrites an entry rewrites
// it for both. Contiguous runs are what make that possible, and what hold a group's attributes to
// R+S-1 resource and G+S-1 scope copies over S shards: repeated only where a boundary falls inside
// the group.
func shardNested(stream logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int) []logproto.InternalStreamAdapter {
	total := stream.EntryCount()
	if total == 0 || shards < 1 {
		return nil
	}

	// No more shards than there are entries to fill them. The numbering still wraps at the count
	// asked for, so a push too small to fill them all leaves the next one carrying on rather than
	// landing on the same few.
	count := shards
	if count > total {
		count = total
	}

	// The remainder is spread over the leading shards rather than left to the last one.
	base, remainder := total/count, total%count
	shardQuota := func(shard int) int {
		if shard < remainder {
			return base + 1
		}
		return base
	}

	// The shard label goes in once as a placeholder, so naming a shard is a string replacement
	// rather than a label set rebuilt and rendered per shard.
	template := labels.NewBuilder(lbls).Set(ingester.ShardLbName, ingester.ShardLbPlaceholder).Labels()
	pattern := template.String()

	out := make([]logproto.InternalStreamAdapter, count)
	for i := range out {
		out[i].Labels, out[i].Hash = shardIdentity(template, pattern, (startShard+i)%shards)
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
				if placed == shardQuota(shard) && shard+1 < count {
					shard, placed = shard+1, 0
					resource, scope = nil, nil
				}

				acceptEntries := len(remaining)
				// If it is not the last shard, limit the entries we take for the shard up to its remaining capacity.
				if shardCapacityLeft := shardQuota(shard) - placed; shard+1 < count && acceptEntries > shardCapacityLeft {
					acceptEntries = shardCapacityLeft
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
				scope.Entries = remaining[:acceptEntries:acceptEntries]
				remaining = remaining[acceptEntries:]
				placed += acceptEntries
			}
		}
	}
	return out
}

// shardIdentity is the name and hash a shard takes from its number.
func shardIdentity(template labels.Labels, pattern string, shardNumber int) (string, uint64) {
	shardLabel := strconv.Itoa(shardNumber)

	return strings.Replace(pattern, ingester.ShardLbPlaceholder, shardLabel, 1),
		labels.StableHash(labels.NewBuilder(template).Set(ingester.ShardLbName, shardLabel).Labels())
}
