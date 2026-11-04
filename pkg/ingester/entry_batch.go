package ingester

import (
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"slices"
)

// entryWithMetadata references the decoded entry so validation can compact a
// batch without copying entries or expanding their shared attributes.
type entryWithMetadata struct {
	*logproto.Entry
	shared *entryMetadata
}

type entryMetadata struct {
	attrs   []logproto.LabelAdapter
	symbols *chunkenc.SharedMetadata
}

func flatEntryBatch(entries []logproto.Entry) []entryWithMetadata {
	batch := make([]entryWithMetadata, len(entries))
	for i := range entries {
		batch[i].Entry = &entries[i]
	}
	return batch
}

func nestedEntryBatch(stream *logproto.InternalStreamAdapter) []entryWithMetadata {
	batch := make([]entryWithMetadata, 0, stream.EntryCount())
	stream.EachGroup(func(resource, scope []logproto.LabelAdapter, entries []logproto.Entry) {
		var shared *entryMetadata
		if len(resource)+len(scope) > 0 {
			attrs := logproto.AppendMetadata(nil, scope, resource)
			shared = &entryMetadata{attrs: attrs, symbols: chunkenc.NewSharedMetadata(attrs)}
		}
		for i := range entries {
			batch = append(batch, entryWithMetadata{Entry: &entries[i], shared: shared})
		}
	})
	return batch
}

func (e entryWithMetadata) sharedAttrs() []logproto.LabelAdapter {
	if e.shared == nil {
		return nil
	}
	return e.shared.attrs
}

func (e entryWithMetadata) totalSize() int {
	return len(e.Line) + e.metadataSize(true)
}

func (e entryWithMetadata) metadataSize(accounted bool) int {
	size := 0
	add := func(attr logproto.LabelAdapter) {
		if !accounted || !slices.Contains(util.ExcludedStructuredMetadataLabels, attr.Name) {
			size += len(attr.Name) + len(attr.Value)
		}
	}
	for _, attr := range e.StructuredMetadata {
		add(attr)
	}
	for _, attr := range e.sharedAttrs() {
		found := false
		for _, own := range e.StructuredMetadata {
			if own.Name == attr.Name {
				found = true
				break
			}
		}
		if !found {
			add(attr)
		}
	}
	return size
}

func (e entryWithMetadata) matches(last line) bool {
	if !e.Timestamp.Equal(last.ts) || e.Line != last.content {
		return false
	}
	if e.shared == nil && len(last.sharedMetadata) == 0 {
		return labelsEqual(e.StructuredMetadata, last.structuredMetadata)
	}
	if labelsEqual(e.StructuredMetadata, last.structuredMetadata) && labelsEqual(e.sharedAttrs(), last.sharedMetadata) {
		return true
	}
	return labelsEqual(
		logproto.AppendMetadata(nil, e.StructuredMetadata, e.sharedAttrs()),
		logproto.AppendMetadata(nil, last.structuredMetadata, last.sharedMetadata),
	)
}

func flattenEntries(entries []entryWithMetadata) []logproto.Entry {
	flat := make([]logproto.Entry, len(entries))
	for i, entry := range entries {
		flat[i] = *entry.Entry
		if entry.shared != nil {
			flat[i].StructuredMetadata = logproto.AppendMetadata(nil, entry.StructuredMetadata, entry.shared.attrs)
		}
	}
	return flat
}
