package util //nolint:revive

import (
	"slices"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func EntriesTotalSize(entries []push.Entry, resourceAndScopeAttributes push.LabelsAdapter) int {
	size := 0
	for _, entry := range entries {
		size += EntryTotalSize(&entry, resourceAndScopeAttributes)
	}
	return size
}

func EntryTotalSize(entry *push.Entry, resourceAndScopeAttributes push.LabelsAdapter) int {
	return len(entry.Line) + StructuredMetadataSize(entry.StructuredMetadata, resourceAndScopeAttributes)
}

var ExcludedStructuredMetadataLabels = []string{constants.LevelLabel}

// StructuredMetadataSize calculates total size of structured metadata excluding ExcludedStructuredMetadataLabels and the given resourceAndScopeAttributes.
func StructuredMetadataSize(metas, resourceAndScopeAttributes push.LabelsAdapter) int {
	size := 0
	for _, meta := range metas {
		if slices.Contains(ExcludedStructuredMetadataLabels, meta.Name) {
			continue
		}
		if resourceAndScopeAttributes != nil && slices.Contains(resourceAndScopeAttributes, meta) {
			continue
		}
		size += len(meta.Name) + len(meta.Value)
	}
	return size
}
