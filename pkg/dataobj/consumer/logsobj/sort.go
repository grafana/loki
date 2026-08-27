package logsobj

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// rankedSortKey is the CopyAndSort sidecar for one stream. rank is the rank in this object after sorting by
// StreamOrderKey; independently written objects do not share rank.
type rankedSortKey struct {
	rank int64
	streams.SortKey
}

// emptyRankedSortKey returns an unranked sort key (rank set to 0) for a label set.
// SortKey fields are calculated from input labels ls.
func emptyRankedSortKey(ls labels.Labels, schemaLabels []string) (rankedSortKey, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return rankedSortKey{}, err
	}

	return rankedSortKey{
		SortKey: streams.NewSortKey(ls, schemaKey),
	}, nil
}

// TargetSortLayout returns the physical logs layout produced for schemaLabels.
func TargetSortLayout(schemaLabels []string) logs.SortLayout {
	return logs.SortLayout{
		SchemaLabels: slices.Clone(schemaLabels),
		StreamOrder:  logs.StreamOrderStableHashV1,
		ShardCount:   streams.ShardFactor,
	}
}

// CompareSortLayout reports whether two physical logs layouts are identical.
func CompareSortLayout(a, b logs.SortLayout) bool {
	return slices.Equal(a.SchemaLabels, b.SchemaLabels) &&
		a.StreamOrder == b.StreamOrder &&
		a.ShardCount == b.ShardCount
}

// sortKeys extracts the sort-tuple column for the k-way merge comparator.
func sortKeys(remap []rankedSortKey) []streams.SortKey {
	out := make([]streams.SortKey, len(remap))
	for i, entry := range remap {
		out[i] = entry.SortKey
	}
	return out
}

// remapByRank returns an identity stream-ID remap indexed by rank.
func remapByRank(remap []rankedSortKey) []rankedSortKey {
	byRank := make([]rankedSortKey, len(remap))
	for _, mapping := range remap {
		if mapping.rank > 0 {
			byRank[mapping.rank] = mapping
		}
	}
	return byRank
}

// mergeAndRemapLogsIter merges sections which are ordered by the sort keys in
// remap, injects sort-key sidecars, and rewrites their stream IDs to rank.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, remap []rankedSortKey) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, sortKeys(remap))
	if err != nil {
		return nil, err
	}

	return result.Iter(func(yield func(logs.Record) bool) error {
		for res := range iter {
			rec, err := res.Value()
			if err != nil {
				return err
			}
			oldStreamID := rec.StreamID
			if oldStreamID <= 0 || oldStreamID >= int64(len(remap)) || remap[oldStreamID].rank == 0 {
				return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
			}
			entry := remap[oldStreamID]
			rec.SchemaKey = entry.SchemaKey
			rec.ShardBucket = entry.ShardBucket
			rec.StreamHash = entry.Hash
			rec.StreamID = entry.rank
			if !yield(rec) {
				return nil
			}
		}
		return nil
	}), nil
}

// replaySections rewrites arbitrary records into bounded, individually sorted
// sections. It returns those sections and their rank-indexed identity remap,
// ready for mergeAndRemapLogsIter.
func (b *Builder) replaySections(ctx context.Context,
	tenant string,
	sections []*dataobj.Section,
	remap []rankedSortKey,
) ([]*dataobj.Section, []rankedSortKey, io.Closer, error) {
	objBuilder := dataobj.NewBuilder(nil)
	intermediateSectionBuilder := logs.NewBuilder(b.metrics.logs, logs.BuilderOptions{
		PageSizeHint:     int(b.cfg.TargetPageSize),
		PageMaxRowCount:  b.cfg.MaxPageRows,
		BufferSize:       int(b.cfg.BufferSize),
		StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        logs.SortStreamASC,
	})
	intermediateSectionBuilder.SetTenant(tenant)

	flushSection := func() error {
		if intermediateSectionBuilder.UncompressedSize() == 0 {
			return nil
		}
		if err := objBuilder.Append(intermediateSectionBuilder); err != nil {
			return err
		}
		intermediateSectionBuilder.Reset()
		intermediateSectionBuilder.SetTenant(tenant)
		return nil
	}

	for _, section := range sections {
		opened, err := logs.Open(ctx, section)
		if err != nil {
			return nil, nil, nil, err
		}
		for res := range logs.IterSection(ctx, opened) {
			rec, err := res.Value()
			if err != nil {
				return nil, nil, nil, err
			}
			if rec.StreamID <= 0 || rec.StreamID >= int64(len(remap)) || remap[rec.StreamID].rank == 0 {
				return nil, nil, nil, fmt.Errorf("missing stream ID remap for stream ID %d", rec.StreamID)
			}
			rec.StreamID = remap[rec.StreamID].rank
			rec.Line = append([]byte(nil), rec.Line...)
			rec.Metadata = rec.Metadata.Copy()
			intermediateSectionBuilder.Append(rec)

			// Intermediate builder uses smaller sections (of BufferSize) so they can be independently compressed
			if intermediateSectionBuilder.UncompressedSize() >= int(b.cfg.BufferSize) {
				if err := flushSection(); err != nil {
					return nil, nil, nil, err
				}
			}
		}
	}
	if err := flushSection(); err != nil {
		return nil, nil, nil, err
	}

	obj, closer, err := objBuilder.Flush()
	if err != nil {
		return nil, nil, nil, err
	}
	var replayedSections []*dataobj.Section
	for _, section := range obj.Sections().Filter(logs.CheckSection) {
		replayedSections = append(replayedSections, section)
	}
	return replayedSections, remapByRank(remap), closer, nil
}
