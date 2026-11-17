package logsobj

import (
	"bytes"
	"context"
	"fmt"
	"slices"

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

// sortedStream is one stream of a tenant in the sorted object.
type sortedStream struct {
	// Stream is the stream as the sorted object holds it. Its ID is the new ID.
	// Rows and UncompressedSize start at 0, and the sort adds each record that
	// it appends. They exclude the duplicates that the sort drops.
	streams.Stream

	oldID int64
	key   streams.SortKey

	// size is the number of uncompressed bytes that the logs of the stream add
	// to a logs section. It comes from the stream stats of the input, so it
	// counts duplicates that the sort can still drop. It is an upper bound.
	size int64
}

// streamTable holds the streams of one tenant. It is read-only while the ranges
// of the sort run, except for the counters of each stream, which only the range
// of the stream updates.
type streamTable struct {
	// sorted holds the streams in sort order. sorted[i].ID is i+1.
	sorted []sortedStream

	// byOldID and keysByOldID are indexed by old ID, where index 0 is unused.
	// A nil entry in byOldID means that the streams section has no such stream.
	byOldID     []*sortedStream
	keysByOldID []streams.SortKey
}

// streamSortKey returns the sort key of a stream with the labels ls.
func streamSortKey(ls labels.Labels, schemaLabels []string, shardLayout streams.ShardLayout) (streams.SortKey, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return streams.SortKey{}, err
	}
	return shardLayout.NewSortKey(ls, schemaKey), nil
}

// newStreamTable orders the streams in iter by the globally stable stream order
// and gives them the new IDs 1 to numStreams in that order. numStreams is the
// number of rows of the streams section.
//
// Stream IDs are originally assigned in the order streams are first recorded.
// After ordering, streams are clustered by [shard_bucket, sort-schema, hash],
// but their old IDs may no longer be monotonic in that order.
//
// Reassigning IDs in stream-order makes the persisted sort metadata
// [streamID ASC, timestamp DESC] match the physical row order and improves
// compression and pruning at query time.
func newStreamTable(iter result.Seq[streams.Stream], tenant string, schemaLabels []string, shardLayout streams.ShardLayout, numStreams int) (*streamTable, error) {
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

	table := &streamTable{
		sorted:      sorted,
		byOldID:     make([]*sortedStream, numStreams+1),
		keysByOldID: make([]streams.SortKey, numStreams+1),
	}
	for i := range sorted {
		s := &sorted[i]
		if s.oldID <= 0 || s.oldID > int64(numStreams) {
			return nil, fmt.Errorf("stream id %d out of range for tenant %s with %d streams", s.oldID, tenant, numStreams)
		}
		if prev := table.byOldID[s.oldID]; prev != nil {
			return nil, fmt.Errorf("duplicate stream id for tenant %s: old id %d maps to both %d and %d", tenant, s.oldID, prev.ID, i+1)
		}
		s.ID = int64(i + 1)
		table.byOldID[s.oldID] = s
		table.keysByOldID[s.oldID] = s.key
	}
	return table, nil
}

// sortPlan says how to sort the logs of one tenant.
type sortPlan struct {
	// table holds the streams of the tenant in sort order.
	table *streamTable

	// ranges splits table.sorted into groups of consecutive streams. The sort
	// handles the groups independently, each on one goroutine, and each group
	// fills about one logs section. They are sub-slices of table.sorted.
	ranges [][]sortedStream
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
// which are ordered by the sort keys of the table. It injects the sort-key
// sidecars of each stream, and rewrites the old stream IDs of the records to
// the new IDs.
//
// rangeStreams must not be empty. Each range of the sort merges its own streams, so
// that the goroutines of the sort never read or write the same record. The
// section readers use the old IDs of the streams to skip the rows of other
// streams. A record of a stream that no range lists does not reach the merge,
// so the caller must give every stream of the tenant to some range.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, table *streamTable, rangeStreams []sortedStream) (result.Seq[sortedRecord], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, table.keysByOldID, oldIDs(rangeStreams))
	if err != nil {
		return nil, err
	}

	return result.Iter(func(yield func(sortedRecord) bool) error {
		for res := range iter {
			rec, err := res.Value()
			if err != nil {
				return err
			}
			if rec.StreamID <= 0 || rec.StreamID >= int64(len(table.byOldID)) || table.byOldID[rec.StreamID] == nil {
				return fmt.Errorf("missing stream ID remap for stream ID %d", rec.StreamID)
			}
			s := table.byOldID[rec.StreamID]
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

// planSortRanges splits the streams, which must be in sort order, into
// contiguous ranges of about targetSize uncompressed bytes, so that each range
// fills about one logs section. The sort gives each range to one goroutine. A
// range takes a new stream only while its total stays within targetSize, with
// two exceptions. A stream larger than targetSize gets a range of its own.
// Streams with the same sort key stay together, so such a range can exceed
// targetSize. The ranges are sub-slices of sorted, so they share its memory.
//
// The sort never splits a stream between ranges. So a tenant that holds most of
// its bytes in one stream sorts that stream on one goroutine, and more
// goroutines do not make it faster.
//
// Example with a targetSize of 100:
//
//	stream  1   2   3   4   5
//	size   60  30  50  80  10
//
// Streams 1 and 2 fill 90 bytes, and stream 3 would take them over 100, so the
// first range is streams 1 and 2. Stream 3 and stream 4 together are 130 bytes,
// so the second range is stream 3. The last range is streams 4 and 5.
//
// A range ends only between two streams with different sort keys [shard,
// schema key, hash]. Two streams can share a sort key when their label hashes
// collide. The merge orders such streams by their old IDs, which can differ
// from their new IDs. For example, streams X and Y share a key. X has new ID 1
// and old ID 9, and Y has new ID 2 and old ID 4. A single merge returns the
// records of Y before the records of X. If X and Y sat in two ranges, the
// sections of X would come first, and the order of the records would change.
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
	)

	return parallel.ForEachOrdered(ctx, len(plan.ranges), parallelism,
		func(ctx context.Context, i int) ([]*encodedSection, error) {
			var lb *logs.Builder
			select {
			case lb = <-free:
			default:
				lb = b.newSectionBuilder(tenant)
			}
			encoded, err := b.sortRange(ctx, tenant, originalLogs, plan.table, plan.ranges[i], lb)
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
		// ForEachOrdered calls yield on one goroutine, in range order. b.builder
		// needs both.
		func(i int, encoded []*encodedSection) error {
			for _, sec := range encoded {
				if err := b.builder.Append(sec); err != nil {
					return fmt.Errorf("appending the sections of stream range %d of tenant %s: %w", i, tenant, err)
				}
			}
			return nil
		})
}

// newSectionBuilder returns an empty logs section builder for the tenant.
//
// drainLogsIter drops duplicates itself when DropDuplicates is set, so the
// section builder must not drop any. Otherwise the stream counters would count
// records that the builder drops.
func (b *Builder) newSectionBuilder(tenant string) *logs.Builder {
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
	return lb
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
	sections []*dataobj.Section,
	table *streamTable,
	rangeStreams []sortedStream,
	lb *logs.Builder,
) ([]*encodedSection, error) {
	iter, err := mergeAndRemapLogsIter(ctx, sections, table, rangeStreams)
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
