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

// rankedStream holds what the sort needs to know about one stream: its sort key,
// its rank and its size.
//
// A stream is one label set of a tenant, one row of its streams section. Before
// the sort, the logs sections refer to a stream by its old ID. In the sorted
// object, the streams of a tenant get the new IDs 1 to N in sort order. The
// rank of a stream is this new ID, so the stream with rank 1 sorts first. A
// rank is unique only among the streams of one tenant in one sorted object.
type rankedStream struct {
	rank int64
	// size is the number of uncompressed bytes that the logs of the stream add
	// to a logs section. It comes from the stream stats, so it counts duplicates
	// that the sort can still drop. It is an upper bound.
	size int64
	streams.SortKey
}

// emptyRankedStream returns an unranked stream (rank and size set to 0) for a
// label set. SortKey fields are calculated from input labels ls.
func emptyRankedStream(ls labels.Labels, schemaLabels []string, shardLayout streams.ShardLayout) (rankedStream, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return rankedStream{}, err
	}

	return rankedStream{
		SortKey: shardLayout.NewSortKey(ls, schemaKey),
	}, nil
}

// sortKeys returns the sort key of each stream in remap, indexed by old ID, for
// the k-way merge comparator.
func sortKeys(remap []rankedStream) []streams.SortKey {
	out := make([]streams.SortKey, len(remap))
	for i, entry := range remap {
		out[i] = entry.SortKey
	}
	return out
}

// mergeAndRemapLogsIter merges sections which are ordered by the sort keys in
// remap, injects sort-key sidecars, and rewrites their stream IDs to rank.
//
// It merges only the records of the streams in streamIDs, which must not be
// empty. Each range of the sort merges its own streams, so that the goroutines
// of the sort never read or write the same record. streamIDs holds old IDs, and
// the section readers use them to skip the rows of other streams. A record of a
// stream that no range lists does not reach the merge, so the caller must give
// every stream of the tenant to some range.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, remap []rankedStream, streamIDs []int64) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, sortKeys(remap), streamIDs)
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

// rankRange is the inclusive range [first, last] of ranks. The sort gives each
// range to one goroutine.
//
// The sort never splits a stream between ranges. So a tenant that holds most of
// its bytes in one stream sorts that stream on one goroutine, and more
// goroutines do not make it faster.
type rankRange struct {
	first, last int64
}

// planSortRanges splits the streams with ranks 1 to len(byRank)-1 into
// contiguous ranges of about targetSize uncompressed bytes, so that each range
// fills about one logs section. byRank is indexed by rank, and index 0 is
// unused. A range takes a new stream only while its total stays within
// targetSize, with two exceptions. A stream larger than targetSize gets a range
// of its own. Streams with the same sort key stay together, so such a range can
// exceed targetSize.
//
// Example with a targetSize of 100:
//
//	rank   1   2   3   4   5
//	size  60  30  50  80  10
//
// Streams 1 and 2 fill 90 bytes, and stream 3 would take them over 100, so the
// first range is [1, 2]. Stream 3 and stream 4 together are 130 bytes, so the
// second range is [3, 3]. The last range is [4, 5].
//
// A range ends only between two streams with different sort keys [shard,
// schema key, hash]. Two streams can share a sort key when their label hashes
// collide. The merge orders such streams by their old IDs, which can differ
// from their ranks. For example, streams X and Y share a key. X has rank 1 and
// old ID 9, and Y has rank 2 and old ID 4. A single merge returns the records of
// Y before the records of X. If X and Y sat in two ranges, the sections of X
// would come first, and the order of the records would change.
func planSortRanges(byRank []rankedStream, targetSize int64) []rankRange {
	last := int64(len(byRank) - 1)
	if last < 1 {
		return nil
	}

	var (
		ranges []rankRange
		first  = int64(1)
		size   int64
	)
	for rank := int64(1); rank < last; rank++ {
		size += byRank[rank].size
		if size+byRank[rank+1].size > targetSize && byRank[rank].Compare(byRank[rank+1].SortKey) != 0 {
			ranges = append(ranges, rankRange{first: first, last: rank})
			first, size = rank+1, 0
		}
	}
	return append(ranges, rankRange{first: first, last: last})
}

// rankedStreamsByRank returns the streams in remap, indexed by rank, where
// index 0 is unused. remap is indexed by old ID, and holds no stream at index 0.
func rankedStreamsByRank(remap []rankedStream) []rankedStream {
	byRank := make([]rankedStream, len(remap))
	for _, entry := range remap {
		if entry.rank > 0 {
			byRank[entry.rank] = entry
		}
	}
	return byRank
}

// streamIDsByRange returns, for each range, the old IDs of the streams whose
// rank falls in that range. remap is indexed by old ID. ranges must be sorted
// and contiguous, and must cover every rank in remap. streamIDsByRange returns
// an error if a rank is in no range.
func streamIDsByRange(remap []rankedStream, ranges []rankRange) ([][]int64, error) {
	ids := make([][]int64, len(ranges))
	for id, entry := range remap {
		if entry.rank <= 0 {
			continue
		}
		i, found := slices.BinarySearchFunc(ranges, entry.rank, func(r rankRange, rank int64) int {
			switch {
			case r.last < rank:
				return -1
			case r.first > rank:
				return 1
			default:
				return 0
			}
		})
		if !found {
			return nil, fmt.Errorf("stream with old ID %d and rank %d is in no range", id, entry.rank)
		}
		ids[i] = append(ids[i], int64(id))
	}
	return ids, nil
}

// sortRangesInParallel sorts the records of each range on up to
// b.cfg.SortParallelism goroutines, and appends their sections to b.builder in
// range order. rangeIDs holds the old IDs of the streams of each range. It
// returns the totals of the appended records, indexed by rank, for ranks 1 to
// numStreams.
//
// At most SortParallelism ranges hold records or encoded sections at a time,
// even when an early range is slow. A range keeps all of its encoded sections in
// memory until the range before it is in b.builder, and b.builder writes them to
// the scratch store only then. So a range with a stream much larger than the
// target section size holds several sections in memory.
//
// The ranges reuse their section builders, so that a builder keeps the
// capacity of its record buffer from one range to the next.
func (b *Builder) sortRangesInParallel(
	ctx context.Context,
	tenant string,
	sections []*dataobj.Section,
	remap []rankedStream,
	rangeIDs [][]int64,
	numStreams int,
	newSection func() *logs.Builder,
) (streamTotals, error) {
	var (
		parallelism = max(1, b.cfg.SortParallelism)
		totals      = newStreamTotals(numStreams)
		free        = make(chan *logs.Builder, parallelism)
	)

	err := parallel.ForEachOrdered(ctx, len(rangeIDs), parallelism,
		func(ctx context.Context, i int) ([]*encodedSection, error) {
			var lb *logs.Builder
			select {
			case lb = <-free:
			default:
				lb = newSection()
			}
			encoded, err := b.sortRange(ctx, tenant, sections, remap, rangeIDs[i], lb, totals)
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
	if err != nil {
		return nil, err
	}
	return totals, nil
}

// sortRange merges the records of the streams in streamIDs, given by old ID, and
// encodes them into sections, each about the target section size. It adds the
// records to totals. lb must be empty and hold the tenant. After success,
// sortRange leaves lb empty and holding the tenant. After an error, lb is in an
// unknown state, and the caller must drop it. sortRange does not use b.builder,
// so it is safe to run on several goroutines at once, as long as their streams
// differ.
func (b *Builder) sortRange(
	ctx context.Context,
	tenant string,
	sections []*dataobj.Section,
	remap []rankedStream,
	streamIDs []int64,
	lb *logs.Builder,
	totals streamTotals,
) ([]*encodedSection, error) {
	iter, err := mergeAndRemapLogsIter(ctx, sections, remap, streamIDs)
	if err != nil {
		return nil, fmt.Errorf("creating sort iterator: %w", err)
	}

	recorder := sectionRecorder{typ: lb.Type()}
	flush := func(lb *logs.Builder) error {
		_, err := lb.Flush(&recorder)
		return err
	}
	if err := b.drainLogsIter(ctx, iter, lb, tenant, totals, flush); err != nil {
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
