package distributor

import (
	"strconv"
	"strings"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// shardNested divides entries into contiguous, equal-count shards.
// Shared attributes repeat at shard boundaries; entry slices alias the input.
func shardNested(stream logproto.InternalStreamAdapter, shards int) []logproto.InternalStreamAdapter {
	total := stream.EntryCount()
	if total == 0 || shards < 1 {
		return nil
	}

	count := min(shards, total)
	out := make([]logproto.InternalStreamAdapter, count)
	base, remainder := total/count, total%count
	shard, placed := 0, 0

	for _, sourceResource := range stream.ResourceLogs {
		var resource *logproto.ResourceLogs
		for _, sourceScope := range sourceResource.ScopeLogs {
			remaining := sourceScope.Entries
			for len(remaining) > 0 {
				quota := base
				if shard < remainder {
					quota++
				}
				if placed == quota && shard+1 < count {
					shard++
					placed = 0
					resource = nil
					continue
				}
				acceptEntries := min(len(remaining), quota-placed)
				resource = appendShardEntries(&out[shard], resource, sourceResource.Attrs, sourceScope.Attrs, remaining[:acceptEntries])
				remaining = remaining[acceptEntries:]
				placed += acceptEntries
			}
		}
	}
	return out
}

// packNested groups consecutive entries by expanded size within the shard limit.
// targetBytes must be positive; large pushes remain byte-balanced across available shards.
// Shared attributes repeat across boundaries; entry slices alias the input.
func packNested(stream logproto.InternalStreamAdapter, shards, expandedBytes int, targetBytes uint64) []logproto.InternalStreamAdapter {
	total := stream.EntryCount()
	if total == 0 || shards < 1 {
		return nil
	}

	count := min(shards, total)
	var capacity int
	if uint64(expandedBytes) <= targetBytes {
		count, capacity = 1, 1
	} else {
		capacity = int(min(uint64(count), 1+(uint64(expandedBytes)-1)/targetBytes))
	}
	out := make([]logproto.InternalStreamAdapter, 1, capacity)
	remainingBytes := uint64(expandedBytes)

	// Raise the target to the remaining average when the shard limit requires it.
	targetForRemaining := func() (uint64, bool) {
		if remainingBytes == 0 {
			return targetBytes, false
		}
		remainingShards := count - len(out) + 1
		averageShardBytes := 1 + (remainingBytes-1)/uint64(remainingShards)
		return max(targetBytes, averageShardBytes), averageShardBytes >= targetBytes
	}

	shardTargetBytes, allowTargetOvershoot := targetForRemaining()
	var currentShardBytes uint64
	placed := 0

	for _, sourceResource := range stream.ResourceLogs {
		var resource *logproto.ResourceLogs
		resourceAttrBytes := 0
		if count > 1 {
			resourceAttrBytes = util.StructuredMetadataSize(sourceResource.Attrs)
		}
		for _, sourceScope := range sourceResource.ScopeLogs {
			sharedAttrBytes := 0
			if count > 1 {
				sharedAttrBytes = resourceAttrBytes + util.StructuredMetadataSize(sourceScope.Attrs)
			}
			remaining := sourceScope.Entries
			for len(remaining) > 0 {
				acceptEntries := len(remaining)
				if len(out) < count {
					acceptEntries = 0
					for i := range remaining {
						entryBytes := uint64(sharedAttrBytes + util.EntryTotalSize(&remaining[i]))
						startNewShard := currentShardBytes > shardTargetBytes
						if !startNewShard && entryBytes > shardTargetBytes-currentShardBytes {
							remainingCapacity := shardTargetBytes - currentShardBytes
							overshootBytes := entryBytes - remainingCapacity
							// Allow overshoot only when it is no larger than the unused capacity.
							startNewShard = !allowTargetOvershoot || overshootBytes > remainingCapacity
						}
						if placed+acceptEntries > 0 && startNewShard {
							break
						}
						currentShardBytes += entryBytes
						acceptEntries++
					}
				}
				if acceptEntries > 0 {
					resource = appendShardEntries(&out[len(out)-1], resource, sourceResource.Attrs, sourceScope.Attrs, remaining[:acceptEntries])
					remaining = remaining[acceptEntries:]
					placed += acceptEntries
				}
				if len(remaining) > 0 {
					out = append(out, logproto.InternalStreamAdapter{})
					remainingBytes -= currentShardBytes
					currentShardBytes, placed = 0, 0
					shardTargetBytes, allowTargetOvershoot = targetForRemaining()
					resource = nil
				}
			}
		}
	}
	return out
}

// appendShardEntries creates the resource on first use and preserves the scope's attributes.
// Entry capacity is capped so appends cannot overwrite another shard's entries.
func appendShardEntries(shard *logproto.InternalStreamAdapter, resource *logproto.ResourceLogs, resourceAttrs, scopeAttrs []logproto.LabelAdapter, entries []logproto.Entry) *logproto.ResourceLogs {
	if resource == nil {
		shard.ResourceLogs = append(shard.ResourceLogs, logproto.ResourceLogs{Attrs: resourceAttrs})
		resource = &shard.ResourceLogs[len(shard.ResourceLogs)-1]
	}
	resource.ScopeLogs = append(resource.ScopeLogs, logproto.ScopeLogs{
		Attrs:   scopeAttrs,
		Entries: entries[:len(entries):len(entries)],
	})
	return resource
}

// labelShards assigns identities after the output count is known and reserved.
func labelShards(out []logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int) {
	if len(out) == 0 {
		return
	}
	template := labels.NewBuilder(lbls).Set(constants.StreamShardLabel, ingester.ShardLbPlaceholder).Labels()
	pattern := template.String()
	for i := range out {
		out[i].Labels, out[i].Hash = shardIdentity(template, pattern, (startShard+i)%shards)
	}
}

// shardIdentity returns the labels and hash for a shard number.
func shardIdentity(template labels.Labels, pattern string, shardNumber int) (string, uint64) {
	shardLabel := strconv.Itoa(shardNumber)

	return strings.Replace(pattern, ingester.ShardLbPlaceholder, shardLabel, 1),
		labels.StableHash(labels.NewBuilder(template).Set(constants.StreamShardLabel, shardLabel).Labels())
}
