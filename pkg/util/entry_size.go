package util

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

var excludedStructuredMetadataLabels = []string{constants.LevelLabel}

func StructuredMetadataSize(metas push.LabelsAdapter) int {
	size := 0
	for _, meta := range metas {
		if slices.Contains(excludedStructuredMetadataLabels, meta.Name) {
			continue
		}
		size += len(meta.Name) + len(meta.Value)
	}
	return size
}
