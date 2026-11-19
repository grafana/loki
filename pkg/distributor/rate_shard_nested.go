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

// shardNested divides entries into contiguous, equal-count shards, with any remainder
// assigned to the leading shards. Shard numbers wrap at shards, starting at startShard.
// Shared attributes repeat across shard boundaries; entry slices alias the input.
func shardNested(stream logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int) []logproto.InternalStreamAdapter {
	return shardNestedWithQuotas(stream, lbls, shards, startShard, nil)
}

// shardNestedWithQuotas assigns contiguous runs using per-shard entry counts.
// Nil quotas distribute entries evenly, with the remainder in the leading shards.
func shardNestedWithQuotas(stream logproto.InternalStreamAdapter, lbls labels.Labels, shards, startShard int, quotas []int) []logproto.InternalStreamAdapter {
	total := stream.EntryCount()
	if total == 0 || shards < 1 {
		return nil
	}

	// Limit outputs to nonempty shards, but keep numbering over the full shard range.
	count := shards
	if count > total {
		count = total
	}
	if quotas != nil {
		count = len(quotas)
	}

	// Without explicit quotas, distribute the remainder across the leading shards.
	base, remainder := total/count, total%count
	shardQuota := func(shard int) int {
		if quotas != nil {
			return quotas[shard]
		}
		if shard < remainder {
			return base + 1
		}
		return base
	}

	// Render labels once, then substitute each shard number.
	template := labels.NewBuilder(lbls).Set(constants.StreamShardLabel, ingester.ShardLbPlaceholder).Labels()
	pattern := template.String()

	out := make([]logproto.InternalStreamAdapter, count)
	for i := range out {
		out[i].Labels, out[i].Hash = shardIdentity(template, pattern, (startShard+i)%shards)
	}

	shard, placed := 0, 0
	for _, sourceResource := range stream.ResourceLogs {
		// Create groups only when entries reach them, reopening them at shard boundaries.
		var resource *logproto.ResourceLogs

		for _, sourceScope := range sourceResource.ScopeLogs {
			var scope *logproto.ScopeLogs

			remaining := sourceScope.Entries

			for len(remaining) > 0 {
				if placed == shardQuota(shard) && shard+1 < count {
					shard, placed = shard+1, 0
					resource, scope = nil, nil
				}

				acceptEntries := len(remaining)
				// Respect each quota; the last shard takes the remainder.
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

				// Cap capacity so appends cannot overwrite the next shard's entries.
				scope.Entries = remaining[:acceptEntries:acceptEntries]
				remaining = remaining[acceptEntries:]
				placed += acceptEntries
			}
		}
	}
	return out
}

// shardIdentity returns the labels and hash for a shard number.
func shardIdentity(template labels.Labels, pattern string, shardNumber int) (string, uint64) {
	shardLabel := strconv.Itoa(shardNumber)

	return strings.Replace(pattern, ingester.ShardLbPlaceholder, shardLabel, 1),
		labels.StableHash(labels.NewBuilder(template).Set(constants.StreamShardLabel, shardLabel).Labels())
}

// packedShardQuotas returns entry counts for contiguous shards targeting targetBytes
// in expanded bytes. Entries stay indivisible; the shard limit can raise the target.
func packedShardQuotas(stream logproto.InternalStreamAdapter, expandedBytes, maxShards int, targetBytes uint64) []int {
	entryCount := stream.EntryCount()
	if maxShards <= 0 || entryCount == 0 || targetBytes == 0 {
		return nil
	}
	maxShards = min(maxShards, entryCount)
	if maxShards == 1 || uint64(expandedBytes) <= targetBytes {
		return []int{entryCount}
	}

	quotas := make([]int, 1, min(maxShards, 16))
	remainingEntries := entryCount
	remainingBytes := uint64(expandedBytes)

	// Use the remaining average when it reaches targetBytes, allowing small overshoots.
	targetForRemaining := func() (uint64, bool) {
		if remainingBytes == 0 {
			return targetBytes, false
		}
		remainingShards := maxShards - len(quotas) + 1
		averageShardBytes := 1 + (remainingBytes-1)/uint64(remainingShards)
		return max(targetBytes, averageShardBytes), averageShardBytes >= targetBytes
	}

	shardTargetBytes, allowTargetOvershoot := targetForRemaining()
	var currentShardBytes uint64
	for _, resource := range stream.ResourceLogs {
		resourceAttrBytes := util.StructuredMetadataSize(resource.Attrs)
		for _, scope := range resource.ScopeLogs {
			sharedAttrBytes := resourceAttrBytes + util.StructuredMetadataSize(scope.Attrs)

			for i := range scope.Entries {
				entryBytes := uint64(sharedAttrBytes + util.EntryTotalSize(&scope.Entries[i]))
				shardIndex := len(quotas) - 1
				startNewShard := currentShardBytes > shardTargetBytes
				if !startNewShard && entryBytes > shardTargetBytes-currentShardBytes {
					remainingCapacity := shardTargetBytes - currentShardBytes
					overshootBytes := entryBytes - remainingCapacity
					// Allow overshoot only when it is no larger than the unused capacity.
					startNewShard = !allowTargetOvershoot || overshootBytes > remainingCapacity
				}
				if quotas[shardIndex] > 0 && startNewShard {
					remainingBytes -= currentShardBytes
					quotas = append(quotas, 0)
					shardIndex++
					currentShardBytes = 0
					shardTargetBytes, allowTargetOvershoot = targetForRemaining()
				}
				if len(quotas) == maxShards {
					quotas[shardIndex] = remainingEntries
					return quotas
				}
				quotas[shardIndex]++
				currentShardBytes += entryBytes
				remainingEntries--
			}
		}
	}
	return quotas
}
