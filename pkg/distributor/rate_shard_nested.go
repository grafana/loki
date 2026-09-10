package distributor

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

// shardNested splits a nested stream's entries across shards, in contiguous runs taken in the order
// the groups appear.
//
// A shard carries the resources and scopes its entries came from, with their attributes repeated,
// and one that contributed nothing to a shard is left out of it. Contiguous runs are what keeps
// that cheap: an attribute set is repeated only where a shard boundary falls inside what it belongs
// to. For R resources holding G scopes between them, over S shards, that is at most R+S-1 copies of
// the resource attributes and G+S-1 of the scope attributes. Handing every shard entries from every
// scope, as round-robin does for flat streams, would cost R*S and G*S instead.
//
// A shard takes its entries as subslices of the source rather than copying them, which contiguous
// runs are also what makes possible. Its entries therefore live in the caller's stream,
// so a caller that rewrites an entry rewrites it for both.
func shardNested(stream *logproto.InternalStreamAdapter, shards int) []logproto.InternalStreamAdapter {
	total := nestedEntryCount(stream)
	if total == 0 || shards < 1 {
		return nil
	}

	// No more shards than there are entries to fill them, matching what streamCount does for flat
	// streams.
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

	out := make([]logproto.InternalStreamAdapter, shards)
	for i := range out {
		out[i].Labels = stream.Labels
		out[i].Hash = stream.Hash
	}

	shard, placed := 0, 0
	for _, sourceResource := range stream.ResourceLogs {
		// The resource being filled in the shard being filled, nil until an entry lands in it.
		// That is what keeps an empty one out of a shard, and what reopens it in the next shard
		// when a boundary falls inside it.
		var resource *logproto.ResourceLogs

		for _, sourceScope := range sourceResource.ScopeLogs {
			var scope *logproto.ScopeLogs

			// A shard takes a run of this scope's entries, so it can hold them as a subslice of
			// the source. Crossing into the next shard begins a new run.
			runStart := 0

			for entryIdx := range sourceScope.Entries {
				if placed == shardQuota(shard) && shard+1 < shards {
					shard, placed = shard+1, 0
					resource, scope, runStart = nil, nil, entryIdx
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
				scope.Entries = sourceScope.Entries[runStart : entryIdx+1 : entryIdx+1]
				placed++
			}
		}
	}
	return out
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
