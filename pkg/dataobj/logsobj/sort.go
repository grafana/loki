package logsobj

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/facette/natsort"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/util/parallel"
)

// A stream is one label set of a tenant, one row of its streams section. Before
// the sort, the logs sections refer to a stream by its old ID. In the sorted
// object, the streams of a tenant get the new IDs 1 to N in sort order.

// sort writes originalObj sorted object-wide to b.builder and flushes it.
// layouts must hold the layout of every tenant in originalObj, and every logs
// section of originalObj must follow the layout of its tenant. originalObj must
// stay open until sort returns.
//
// sort always resets Builder.
func (b *Builder) sort(ctx context.Context, originalObj *dataobj.Object, layouts map[string]streams.Layout) (*dataobj.Object, io.Closer, error) {
	defer b.Reset()

	// Sort the set of tenants so the new object has a deterministic order of sections.
	tenants := originalObj.Tenants()
	natsort.Sort(tenants)

	for _, tenant := range tenants {
		tenantLayout, ok := layouts[tenant]
		if !ok {
			return nil, nil, fmt.Errorf("no layout for tenant %s", tenant)
		}
		if err := b.sortAndWriteTenant(ctx, originalObj, tenant, tenantLayout); err != nil {
			return nil, nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return b.builder.Flush()
}

// sortAndWriteTenant sorts the logs and the streams of one tenant in
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
	plan, err := b.planTenantSort(ctx, tenant, originalStreams, tenantLayout)
	if err != nil {
		return err
	}
	if err := b.writeLogsSections(ctx, tenant, originalLogs, plan); err != nil {
		return err
	}
	return b.writeStreamsSection(ctx, tenant, plan, tenantLayout)
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

// sortPlan says how to sort the logs of one tenant.
type sortPlan struct {
	// streams holds the streams of the tenant in sort order, with the new IDs
	// 1 to N.
	streams []sortedStream

	// ranges splits streams into groups of consecutive streams. The sort
	// handles the groups independently, each on one goroutine, and each group
	// fills about one logs section. They are sub-slices of streams.
	ranges [][]sortedStream
}

// planTenantSort orders the streams of the tenant, and cuts them into ranges of
// about one logs section. It returns an error if the tenant has no streams,
// because a sort without ranges would drop every record of its logs sections.
func (b *Builder) planTenantSort(ctx context.Context, tenant string, originalStreams *streams.Section, tenantLayout streams.Layout) (*sortPlan, error) {
	sorted, err := newSortedStreams(streamsSectionIter(ctx, originalStreams), tenant, tenantLayout.SchemaLabels(), tenantLayout.ShardLayout(), originalStreams.NumRows())
	if err != nil {
		return nil, fmt.Errorf("ordering the streams of tenant %s: %w", tenant, err)
	}
	ranges := planSortRanges(sorted, int64(b.cfg.TargetSectionSize))
	if len(ranges) == 0 {
		return nil, fmt.Errorf("tenant %s has logs sections but no streams", tenant)
	}
	return &sortPlan{streams: sorted, ranges: ranges}, nil
}

// writeLogsSections sorts the records of the tenant, and appends the sorted
// logs sections to b.builder. It sorts each range of the plan on up to
// b.cfg.SortParallelism goroutines, and appends the sections of the ranges in
// range order. It adds each appended record to the counters of its stream in
// the plan. originalLogs are the logs sections of the tenant in the object that
// the sort reads.
//
// At most SortParallelism ranges hold records or encoded sections at a time,
// even when an early range is slow. A range keeps all of its encoded sections in
// memory until the range before it is in b.builder, and b.builder writes them to
// the scratch store only then. So a range with a stream much larger than the
// target section size holds several sections in memory. A section builder holds
// the records of a range up to the target section size.
//
// The ranges reuse their section builders, so that a builder keeps the
// capacity of its record buffer from one range to the next.
func (b *Builder) writeLogsSections(
	ctx context.Context,
	tenant string,
	originalLogs []*dataobj.Section,
	plan *sortPlan,
) error {
	var (
		parallelism = max(1, b.cfg.SortParallelism)
		free        = make(chan *logs.Builder, parallelism)
		ordering    = sortKeysByOldID(plan.streams)
	)

	return parallel.ForEachOrdered(ctx, len(plan.ranges), parallelism,
		func(ctx context.Context, i int) ([]*encodedSection, error) {
			var lb *logs.Builder
			select {
			case lb = <-free:
			default:
				// sortRange drops duplicates itself when DropDuplicates is set,
				// so the section builder must not drop any. Otherwise the
				// stream counters would count records that the builder drops.
				lb = logs.NewBuilder(b.sortLogsMetrics, logs.BuilderOptions{
					PageSizeHint:     int(b.cfg.TargetPageSize),
					PageMaxRowCount:  b.cfg.MaxPageRows,
					BufferSize:       int(b.cfg.BufferSize),
					StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
					AppendStrategy:   logs.AppendOrdered,
					SortOrder:        logs.SortSchemaASC,
					DropDuplicates:   false,
				})
				lb.SetTenant(tenant)
			}
			encoded, err := b.sortRange(ctx, tenant, originalLogs, ordering, plan.ranges[i], lb)
			if err != nil {
				return nil, fmt.Errorf("sorting stream range %d of tenant %s: %w", i, tenant, err)
			}
			// Only a range that succeeded gives its builder back. Its last flush
			// reset the builder, so the next range starts clean. A failed range
			// drops its builder. free has room for every builder, because at
			// most parallelism ranges run at a time.
			free <- lb
			return encoded, nil
		},
		// b.builder is not safe for concurrent use, and it needs the sections in
		// range order. ForEachOrdered calls this on one goroutine, in range order.
		func(i int, encoded []*encodedSection) error {
			for _, sec := range encoded {
				if err := b.builder.Append(sec); err != nil {
					return fmt.Errorf("appending the sections of stream range %d of tenant %s: %w", i, tenant, err)
				}
			}
			return nil
		})
}

// sortRange merges the records of the streams in a range and encodes them into
// sections, each about the target section size. It adds the records to the
// counters of the streams. lb must be empty and hold the tenant. After success,
// sortRange leaves lb empty and holding the tenant. After an error, lb is in an
// unknown state, and the caller must drop it. sortRange does not use b.builder,
// so it is safe to run on several goroutines at once, as long as their ranges
// differ.
func (b *Builder) sortRange(
	ctx context.Context,
	tenant string,
	originalLogs []*dataobj.Section,
	ordering []streams.SortKey,
	rangeStreams []sortedStream,
	lb *logs.Builder,
) ([]*encodedSection, error) {
	iter, err := mergeAndRemapLogsIter(ctx, originalLogs, ordering, rangeStreams)
	if err != nil {
		return nil, fmt.Errorf("creating sort iterator: %w", err)
	}

	recorder := sectionRecorder{typ: lb.Type()}
	flush := func(lb *logs.Builder) error {
		_, err := lb.Flush(&recorder)
		return err
	}
	if err := b.drainLogsIter(ctx, iter, lb, tenant, flush); err != nil {
		return nil, err
	}
	return recorder.sections, nil
}

// drainLogsIter consumes iter and appends each record to lb. It passes lb to
// flush whenever the section size target is exceeded and once more after iter
// ends, and it resets lb after each flush. It adds each appended record to the
// counters of its stream.
//
// If DropDuplicates is set, drainLogsIter skips duplicate records. iter must
// yield the records of each stream and timestamp together, as
// [logs.DuplicateTracker] requires.
//
// drainLogsIter changes only the counters of the streams in iter, so several
// calls can run at the same time if their streams differ.
func (b *Builder) drainLogsIter(
	ctx context.Context,
	iter result.Seq[sortedRecord],
	lb *logs.Builder,
	tenant string,
	flush func(*logs.Builder) error,
) error {
	flushAndReset := func() error {
		if err := flush(lb); err != nil {
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
			if err := flushAndReset(); err != nil {
				return err
			}
		}
	}
	return flushAndReset()
}

// writeStreamsSection appends the sorted streams section of the tenant to
// b.builder. It takes the streams from the plan, with the rows and the sizes
// that writeLogsSections counted.
func (b *Builder) writeStreamsSection(ctx context.Context, tenant string, plan *sortPlan, tenantLayout streams.Layout) error {
	sb := streams.NewBuilder(b.sortStreamsMetrics, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, tenantLayout.ShardLayout())
	sb.SetLayout(tenantLayout)
	sb.SetTenant(tenant)

	for i := range plan.streams {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sb.AppendValue(plan.streams[i].Stream)
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

	// size is the number of bytes that the records of the stream add to the size
	// of a logs section builder. That is the uncompressed size of the stream
	// plus [logs.RecordOverhead] for each row. It comes from the stream stats of
	// the input, which can count duplicates that the sort drops, so it is an
	// upper bound.
	size int64
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
	sorted := make([]sortedStream, 0, numStreams)
	for res := range iter {
		stream, err := res.Value()
		if err != nil {
			return nil, err
		}
		key, err := streamSortKey(stream.Labels, schemaLabels, shardLayout)
		if err != nil {
			return nil, err
		}
		s := sortedStream{
			Stream: stream,
			oldID:  stream.ID,
			key:    key,
			size:   stream.UncompressedSize + int64(stream.Rows)*logs.RecordOverhead,
		}
		s.Rows, s.UncompressedSize = 0, 0
		sorted = append(sorted, s)
	}

	slices.SortFunc(sorted, func(a, b sortedStream) int {
		return streams.CompareSortKey(a.key, b.key)
	})

	newIDs := make(map[int64]int64, numStreams)
	for i := range sorted {
		s := &sorted[i]
		if s.oldID <= 0 || s.oldID > int64(numStreams) {
			return nil, fmt.Errorf("stream id %d out of range for tenant %s with %d streams", s.oldID, tenant, numStreams)
		}
		if prev, ok := newIDs[s.oldID]; ok {
			return nil, fmt.Errorf("duplicate stream id for tenant %s: old id %d maps to both %d and %d", tenant, s.oldID, prev, i+1)
		}
		s.ID = int64(i + 1)
		newIDs[s.oldID] = s.ID
	}
	return sorted, nil
}

// sortKeysByOldID returns the sort key of each stream, indexed by old ID, where
// index 0 is unused. The comparator of the merge takes the keys this way. The
// old IDs must be the numbers 1 to len(sorted), as newSortedStreams checks.
func sortKeysByOldID(sorted []sortedStream) []streams.SortKey {
	keys := make([]streams.SortKey, len(sorted)+1)
	for i := range sorted {
		keys[sorted[i].oldID] = sorted[i].key
	}
	return keys
}

// planSortRanges splits the streams, in sort order, into ranges of consecutive
// streams. A range takes the next stream only while its total section size
// stays within targetSize, so that it fills about one logs section. A stream
// larger than targetSize gets a range of its own, and a stream is never split.
// Streams with the same sort key stay in one range, because the merge orders
// them by old ID, and splitting them would change the order of their records.
//
// For example, with a targetSize of 100:
//   - the section sizes 60, 30, 50, 80, 10 give the ranges {60, 30}, {50}, {80, 10}.
//   - the section sizes 10, 500, 10 give the ranges {10}, {500}, {10}.
func planSortRanges(sorted []sortedStream, targetSize int64) [][]sortedStream {
	var (
		ranges [][]sortedStream
		start  int
		size   int64
	)
	for i := 0; i+1 < len(sorted); i++ {
		size += sorted[i].size
		if size+sorted[i+1].size > targetSize && sorted[i].key.Compare(sorted[i+1].key) != 0 {
			ranges = append(ranges, sorted[start:i+1])
			start, size = i+1, 0
		}
	}
	if start < len(sorted) {
		ranges = append(ranges, sorted[start:])
	}
	return ranges
}

// oldIDs returns the old IDs of the streams.
func oldIDs(list []sortedStream) []int64 {
	ids := make([]int64, len(list))
	for i := range list {
		ids[i] = list[i].oldID
	}
	return ids
}

// sortedRecord is a record of the merge with the stream that owns it. The
// record has the new ID of its stream.
type sortedRecord struct {
	logs.Record
	stream *sortedStream
}

// mergeAndRemapLogsIter merges the records of the given streams from sections,
// which are ordered by the sort keys in ordering, indexed by old ID. It injects
// the sort-key sidecars of each stream, and rewrites the old stream IDs of the
// records to the new IDs.
//
// rangeStreams must not be empty. Each range of the sort merges its own
// streams, so that the goroutines of the sort never read or write the same
// record. The section readers use the old IDs of the streams to skip the rows
// of other streams. A record of a stream that no range lists does not reach the
// merge, so the caller must give every stream of the tenant to some range.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, ordering []streams.SortKey, rangeStreams []sortedStream) (result.Seq[sortedRecord], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, ordering, oldIDs(rangeStreams))
	if err != nil {
		return nil, err
	}

	byOldID := make(map[int64]*sortedStream, len(rangeStreams))
	for i := range rangeStreams {
		byOldID[rangeStreams[i].oldID] = &rangeStreams[i]
	}

	return result.Iter(func(yield func(sortedRecord) bool) error {
		for res := range iter {
			rec, err := res.Value()
			if err != nil {
				return err
			}
			s, ok := byOldID[rec.StreamID]
			if !ok {
				return fmt.Errorf("record of stream %d, which is not in the range", rec.StreamID)
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

// sectionRecorder is a [dataobj.SectionWriter] that keeps a copy of each
// section written to it, so that the section can reach a [dataobj.Builder]
// later. The copies stay in memory.
type sectionRecorder struct {
	typ      dataobj.SectionType
	sections []*encodedSection
}

func (r *sectionRecorder) WriteSection(opts *dataobj.WriteSectionOptions, data, metadata []byte) (int64, error) {
	// The caller may reuse data and metadata after this call, so copy them.
	sec := &encodedSection{
		typ:      r.typ,
		data:     bytes.Clone(data),
		metadata: bytes.Clone(metadata),
	}
	if opts != nil {
		copied := *opts
		copied.ExtensionData = bytes.Clone(opts.ExtensionData)
		sec.opts = &copied
	}
	r.sections = append(r.sections, sec)
	return int64(len(data) + len(metadata)), nil
}

// encodedSection is a [dataobj.SectionBuilder] for a section that is already
// encoded. Flush writes the same bytes each time.
type encodedSection struct {
	typ            dataobj.SectionType
	opts           *dataobj.WriteSectionOptions
	data, metadata []byte
}

func (s *encodedSection) Type() dataobj.SectionType { return s.typ }

func (s *encodedSection) Flush(w dataobj.SectionWriter) (int64, error) {
	return w.WriteSection(s.opts, s.data, s.metadata)
}

func (s *encodedSection) Reset() {}
