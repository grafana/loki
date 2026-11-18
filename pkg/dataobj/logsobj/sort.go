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

// A stream is one label set of a tenant, one row of its streams section. Before
// the sort, the logs sections refer to a stream by its old ID. In the sorted
// object, the streams of a tenant get the new IDs 1 to N in sort order.

// sort writes originalObj sorted object-wide to b.builder and flushes it.
// originalObj must hold exactly one tenant. layouts must hold the layout of
// that tenant, and every logs section of originalObj must follow it.
// originalObj must stay open until sort returns.
//
// sort always resets Builder.
func (b *Builder) sort(ctx context.Context, originalObj *dataobj.Object, layouts map[string]streams.Layout) (*dataobj.Object, io.Closer, error) {
	defer b.Reset()

	tenants := originalObj.Tenants()
	if len(tenants) != 1 {
		return nil, nil, fmt.Errorf("a sorted object holds one tenant, but the builder holds %d", len(tenants))
	}
	tenant := tenants[0]
	tenantLayout, ok := layouts[tenant]
	if !ok {
		return nil, nil, fmt.Errorf("no layout for tenant %s", tenant)
	}
	if err := b.sortAndWriteTenant(ctx, originalObj, tenant, tenantLayout); err != nil {
		return nil, nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return b.builder.Flush()
}

// sortAndWriteTenant sorts the logs and the streams of the tenant in
// originalObj, and appends the sorted logs sections and the sorted streams
// section of the tenant to b.builder.
//
// tenantLayout is the layout that the builder used for the tenant in
// originalObj, and the sorted object keeps it. The logs go first, because the
// streams section needs the rows and the sizes that the sort of the logs
// counts.
func (b *Builder) sortAndWriteTenant(ctx context.Context, originalObj *dataobj.Object, tenant string, tenantLayout streams.Layout) error {
	originalLogs, originalStreams, err := tenantSections(ctx, originalObj, tenant)
	if err != nil {
		return err
	}
	sortedStreams, err := newSortedStreams(streamsSectionIter(ctx, originalStreams), tenant, tenantLayout.SchemaLabels(), tenantLayout.ShardLayout(), originalStreams.NumRows())
	if err != nil {
		return fmt.Errorf("ordering the streams of tenant %s: %w", tenant, err)
	}
	if err := b.writeLogsSections(ctx, tenant, originalLogs, sortedStreams); err != nil {
		return err
	}
	return b.writeStreamsSection(ctx, tenant, sortedStreams, tenantLayout)
}

func tenantSections(ctx context.Context, obj *dataobj.Object, tenant string) ([]*dataobj.Section, *streams.Section, error) {
	var logsSections []*dataobj.Section
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return logs.CheckSection(s) && s.Tenant == tenant }) {
		logsSections = append(logsSections, sec)
	}
	if len(logsSections) == 0 {
		return nil, nil, fmt.Errorf("no logs sections found for tenant: %v", tenant)
	}

	var streamSections []*dataobj.Section
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return streams.CheckSection(s) && s.Tenant == tenant
	}) {
		streamSections = append(streamSections, sec)
	}
	if len(streamSections) == 0 {
		return nil, nil, fmt.Errorf("no streams sections found for tenant: %v", tenant)
	}
	if len(streamSections) > 1 {
		return nil, nil, fmt.Errorf("multiple streams sections found for tenant: %v", tenant)
	}

	streamsSection, err := streams.Open(ctx, streamSections[0])
	if err != nil {
		return nil, nil, fmt.Errorf("opening streams section for tenant %s: %w", tenant, err)
	}
	return logsSections, streamsSection, nil
}

// writeLogsSections merges the records of the tenant from originalLogs in sort
// order, and appends the sorted logs sections to b.builder. It adds each
// appended record to the counters of its stream in sortedStreams.
func (b *Builder) writeLogsSections(ctx context.Context, tenant string, originalLogs []*dataobj.Section, sortedStreams []sortedStream) error {
	byOldID := make(map[int64]*sortedStream, len(sortedStreams))
	for i := range sortedStreams {
		byOldID[sortedStreams[i].oldID] = &sortedStreams[i]
	}
	iter, err := mergeAndRemapLogsIter(ctx, originalLogs, sortKeysByOldID(sortedStreams), byOldID)
	if err != nil {
		return fmt.Errorf("creating sort iterator for tenant %s: %w", tenant, err)
	}

	// drainLogsIter drops duplicates itself when DropDuplicates is set, so the
	// section builder must not drop any. Otherwise the stream counters would
	// count records that the builder drops.
	lb := logs.NewBuilder(b.sortLogsMetrics, logs.BuilderOptions{
		PageSizeHint:     int(b.cfg.TargetPageSize),
		PageMaxRowCount:  b.cfg.MaxPageRows,
		BufferSize:       int(b.cfg.BufferSize),
		StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        logs.SortSchemaASC,
		DropDuplicates:   false,
	})
	lb.SetTenant(tenant)
	return b.drainLogsIter(ctx, iter, lb, tenant)
}

// drainLogsIter consumes iter and appends each record to lb. It appends lb to
// b.builder whenever the section size target is exceeded and once more after
// iter ends, and it resets lb after each append. It adds each appended record to
// the counters of its stream.
//
// If DropDuplicates is set, drainLogsIter skips duplicate records. iter must
// yield the records of each stream and timestamp together, as
// [logs.DuplicateTracker] requires.
func (b *Builder) drainLogsIter(ctx context.Context, iter result.Seq[sortedRecord], lb *logs.Builder, tenant string) error {
	appendAndReset := func() error {
		if err := b.builder.Append(lb); err != nil {
			return err
		}
		lb.Reset()
		lb.SetTenant(tenant)
		return nil
	}

	var duplicates logs.DuplicateTracker
	for res := range iter {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sr, err := res.Value()
		if err != nil {
			return err
		}
		if b.cfg.DropDuplicates && duplicates.IsDuplicate(sr.Record) {
			continue
		}
		sr.stream.Rows++
		sr.stream.UncompressedSize += sr.UncompressedSize()
		lb.Append(sr.Record)
		if lb.UncompressedSize() > int(b.cfg.TargetSectionSize) {
			if err := appendAndReset(); err != nil {
				return err
			}
		}
	}
	return appendAndReset()
}

// writeStreamsSection appends the sorted streams section of the tenant to
// b.builder. It takes the streams from sortedStreams, with the rows and the sizes that
// writeLogsSections counted.
func (b *Builder) writeStreamsSection(ctx context.Context, tenant string, sortedStreams []sortedStream, tenantLayout streams.Layout) error {
	sb := streams.NewBuilder(b.sortStreamsMetrics, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, tenantLayout.ShardLayout())
	sb.SetLayout(tenantLayout)
	sb.SetTenant(tenant)

	for i := range sortedStreams {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sb.AppendValue(sortedStreams[i].Stream)
	}
	return b.builder.Append(sb)
}

// sortedStream is one stream of a tenant in the sorted object.
type sortedStream struct {
	// Stream is the stream as the sorted object holds it. Its ID is the new ID.
	// Rows and UncompressedSize start at 0, and the sort adds each record that
	// it appends. They exclude the duplicates that the sort drops.
	streams.Stream

	oldID int64
	key   streams.SortKey
}

// streamSortKey returns the sort key of a stream with the labels ls.
func streamSortKey(ls labels.Labels, schemaLabels []string, shardLayout streams.ShardLayout) (streams.SortKey, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return streams.SortKey{}, err
	}
	return shardLayout.NewSortKey(ls, schemaKey), nil
}

// newSortedStreams orders the streams in iter by the globally stable stream
// order and gives them the new IDs 1 to numStreams in that order. numStreams is
// the number of rows of the streams section. It returns an error unless the old
// IDs of the streams are the numbers 1 to numStreams, each once.
//
// Stream IDs are originally assigned in the order streams are first recorded.
// After ordering, streams are clustered by [shard_bucket, sort-schema, hash],
// but their old IDs may no longer be monotonic in that order.
//
// Reassigning IDs in stream-order makes the persisted sort metadata
// [streamID ASC, timestamp DESC] match the physical row order and improves
// compression and pruning at query time.
func newSortedStreams(iter result.Seq[streams.Stream], tenant string, schemaLabels []string, shardLayout streams.ShardLayout, numStreams int) ([]sortedStream, error) {
	sortedStreams := make([]sortedStream, 0, numStreams)
	for res := range iter {
		stream, err := res.Value()
		if err != nil {
			return nil, err
		}
		key, err := streamSortKey(stream.Labels, schemaLabels, shardLayout)
		if err != nil {
			return nil, err
		}
		s := sortedStream{Stream: stream, oldID: stream.ID, key: key}
		s.Rows, s.UncompressedSize = 0, 0
		sortedStreams = append(sortedStreams, s)
	}

	slices.SortFunc(sortedStreams, func(a, b sortedStream) int {
		return streams.CompareSortKey(a.key, b.key)
	})

	newIDs := make(map[int64]int64, numStreams)
	for i := range sortedStreams {
		s := &sortedStreams[i]
		if s.oldID <= 0 || s.oldID > int64(numStreams) {
			return nil, fmt.Errorf("stream id %d out of range for tenant %s with %d streams", s.oldID, tenant, numStreams)
		}
		if prev, ok := newIDs[s.oldID]; ok {
			return nil, fmt.Errorf("duplicate stream id for tenant %s: old id %d maps to both %d and %d", tenant, s.oldID, prev, i+1)
		}
		s.ID = int64(i + 1)
		newIDs[s.oldID] = s.ID
	}
	return sortedStreams, nil
}

// sortKeysByOldID returns the sort key of each stream, indexed by old ID, where
// index 0 is unused. The comparator of the merge takes the keys this way. The
// old IDs must be the numbers 1 to len(sortedStreams), as newSortedStreams checks.
func sortKeysByOldID(sortedStreams []sortedStream) []streams.SortKey {
	keys := make([]streams.SortKey, len(sortedStreams)+1)
	for i := range sortedStreams {
		keys[sortedStreams[i].oldID] = sortedStreams[i].key
	}
	return keys
}

// sortedRecord is a record of the merge with the stream that owns it. The
// record has the new ID of its stream.
type sortedRecord struct {
	logs.Record
	stream *sortedStream
}

// mergeAndRemapLogsIter merges sections which are ordered by the sort keys in
// ordering, indexed by old ID. It injects the sort-key sidecars of each stream,
// and rewrites the old stream IDs of the records to the new IDs. byOldID finds
// the stream of a record.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, ordering []streams.SortKey, byOldID map[int64]*sortedStream) (result.Seq[sortedRecord], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, ordering)
	if err != nil {
		return nil, err
	}

	return result.Iter(func(yield func(sortedRecord) bool) error {
		for res := range iter {
			rec, err := res.Value()
			if err != nil {
				return err
			}
			s, ok := byOldID[rec.StreamID]
			if !ok {
				return fmt.Errorf("missing stream ID remap for stream ID %d", rec.StreamID)
			}
			rec.SchemaKey = s.key.SchemaKey
			rec.ShardBucket = s.key.ShardBucket
			rec.StreamHash = s.key.Hash
			rec.StreamID = s.ID
			if !yield(sortedRecord{Record: rec, stream: s}) {
				return nil
			}
		}
		return nil
	}), nil
}

func streamsSectionIter(ctx context.Context, section *streams.Section) result.Seq[streams.Stream] {
	return result.Iter(func(yield func(streams.Stream) bool) error {
		for res := range streams.IterSection(ctx, section) {
			stream, err := res.Value()
			if err != nil {
				return err
			}
			if !yield(stream) {
				return nil
			}
		}
		return nil
	})
}
