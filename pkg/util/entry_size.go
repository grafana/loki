package util //nolint:revive

import (
	"slices"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func EntriesTotalSize(entries []push.Entry) int {
	size := 0
	for _, entry := range entries {
		size += EntryTotalSize(&entry)
	}
	return size
}

func EntryTotalSize(entry *push.Entry) int {
	return len(entry.Line) + StructuredMetadataSize(entry.StructuredMetadata)
}

// EffectiveStructuredMetadataSize returns the structured metadata size an entry accounts for
// once the shared structured metadata sets it references are expanded into it, that is what
// the entry would have measured had the producer copied the shared sets into it.
//
// resourceSize and scopeSize are the sizes of the entry's resource and scope sets. Both are
// expected to be computed once per set with StructuredMetadataSize and looked up per entry,
// since a stream's pool has far fewer sets than entries.
func EffectiveStructuredMetadataSize(entry *push.Entry, resourceSize, scopeSize int) int {
	return StructuredMetadataSize(entry.StructuredMetadata) + resourceSize + scopeSize
}

// EffectiveEntryTotalSize is EntryTotalSize accounting for the shared structured metadata
// sets the entry references. See EffectiveStructuredMetadataSize for the two sizes.
func EffectiveEntryTotalSize(entry *push.Entry, resourceSize, scopeSize int) int {
	return len(entry.Line) + EffectiveStructuredMetadataSize(entry, resourceSize, scopeSize)
}

// SharedSetsSize returns the size of a stream's shared structured metadata pool, counting
// every set once no matter how many entries reference it.
//
// This is the unexpanded, stream level view of the shared metadata: it is what the pool
// actually costs on the wire and in memory, as opposed to the per entry expanded accounting
// EffectiveEntryTotalSize does for limits that have to stay compatible with what an expanded
// push would have measured.
func SharedSetsSize(sets []push.SharedStructuredMetadataSet) int {
	size := 0
	for i := range sets {
		size += StructuredMetadataSize(sets[i].Attrs)
	}
	return size
}

var ExcludedStructuredMetadataLabels = []string{constants.LevelLabel}

func StructuredMetadataSize(metas push.LabelsAdapter) int {
	size := 0
	for _, meta := range metas {
		if slices.Contains(ExcludedStructuredMetadataLabels, meta.Name) {
			continue
		}
		size += len(meta.Name) + len(meta.Value)
	}
	return size
}
