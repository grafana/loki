package util

import "github.com/grafana/loki/pkg/push"

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

func StructuredMetadataSize(metas push.LabelsAdapter) int {
	size := 0
	for _, meta := range metas {
		size += len(meta.Name) + len(meta.Value)
	}
	return size
}
