package logsobj

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// rankRange is the inclusive range [first, last] of stream ranks.
type rankRange struct {
	first, last int64
}

// planSortRanges splits the stream ranks 1 to len(sizes)-1 into contiguous
// ranges of at most targetSize uncompressed bytes each, so that each range
// fills one logs section. sizes and keys are indexed by rank, and index 0 is
// unused. A stream larger than targetSize gets a range of its own.
//
// A range ends only where the sort key [shard, schema key, hash] changes. The
// merge orders streams with equal keys by their stream IDs in the input
// sections, not by rank. So streams with equal keys must stay in one range,
// or the ranges would not be contiguous in merge order.
func planSortRanges(sizes []int64, keys []streams.SortKey, targetSize int64) []rankRange {
	last := int64(len(sizes) - 1)
	if last < 1 {
		return nil
	}

	var (
		ranges []rankRange
		first  = int64(1)
		size   int64
	)
	for rank := int64(1); rank < last; rank++ {
		size += sizes[rank]
		if size+sizes[rank+1] > targetSize && keys[rank].Compare(keys[rank+1]) != 0 {
			ranges = append(ranges, rankRange{first: first, last: rank})
			first, size = rank+1, 0
		}
	}
	return append(ranges, rankRange{first: first, last: last})
}

// sortKeysByRank returns the sort key of each stream in remap, indexed by
// rank. remap is indexed by the stream ID before the remap.
func sortKeysByRank(remap []rankedSortKey) []streams.SortKey {
	keys := make([]streams.SortKey, len(remap))
	for _, entry := range remap {
		if entry.rank > 0 {
			keys[entry.rank] = entry.SortKey
		}
	}
	return keys
}

// streamIDsByRange returns, for each range, the stream IDs of the merged
// sections whose rank falls in that range. remap is indexed by those stream
// IDs. ranges must be sorted and contiguous.
func streamIDsByRange(remap []rankedSortKey, ranges []rankRange) [][]int64 {
	ids := make([][]int64, len(ranges))
	for id, entry := range remap {
		if entry.rank <= 0 {
			continue
		}
		i, _ := slices.BinarySearchFunc(ranges, entry.rank, func(r rankRange, rank int64) int {
			switch {
			case r.last < rank:
				return -1
			case r.first > rank:
				return 1
			default:
				return 0
			}
		})
		ids[i] = append(ids[i], int64(id))
	}
	return ids
}

// sortRangesConcurrently sorts the records of each range on up to
// concurrency goroutines, and appends their sections to b.builder in range
// order. rangeIDs holds the stream IDs of each range, in sections.
//
// A range waits for a slot before it starts, and frees its slot only once
// its sections are in b.builder. So at most concurrency ranges hold buffered
// records or encoded sections at a time, even when an early range is slow.
//
// The ranges reuse their section builders, so that a builder keeps the
// capacity of its record buffer from one range to the next.
func (b *Builder) sortRangesConcurrently(
	ctx context.Context,
	tenant string,
	sections []*dataobj.Section,
	remap []rankedSortKey,
	rangeIDs [][]int64,
	newSection func() *logs.Builder,
	concurrency int,
) error {
	var (
		g, gctx = errgroup.WithContext(ctx)
		slots   = make(chan struct{}, concurrency)
		free    = make(chan *logs.Builder, concurrency)
		sorted  = make([]chan []*encodedSection, len(rangeIDs))
	)
	for i := range sorted {
		sorted[i] = make(chan []*encodedSection, 1)
	}

	g.Go(func() error {
		for i, ids := range rangeIDs {
			select {
			case slots <- struct{}{}:
			case <-gctx.Done():
				return gctx.Err()
			}
			g.Go(func() error {
				var lb *logs.Builder
				select {
				case lb = <-free:
				default:
					lb = newSection()
				}
				encoded, err := b.sortRange(gctx, tenant, sections, remap, ids, lb)
				if err != nil {
					return fmt.Errorf("sorting stream range %d of tenant %s: %w", i, tenant, err)
				}
				// free has room for every builder, because at most
				// concurrency ranges run at a time.
				free <- lb
				sorted[i] <- encoded
				return nil
			})
		}
		return nil
	})

	// Only this goroutine appends to b.builder, which is not safe for
	// concurrent use.
	g.Go(func() error {
		for i := range rangeIDs {
			var encoded []*encodedSection
			select {
			case encoded = <-sorted[i]:
			case <-gctx.Done():
				return gctx.Err()
			}
			for _, sec := range encoded {
				if err := b.builder.Append(sec); err != nil {
					return err
				}
			}
			<-slots
		}
		return nil
	})

	return g.Wait()
}

// sortRange merges the records of the streams in streamIDs and encodes them
// into sections, each about the target section size. lb must be empty and
// hold the tenant; sortRange leaves it empty. sortRange does not use
// b.builder, so it is safe to run on several goroutines at once.
func (b *Builder) sortRange(
	ctx context.Context,
	tenant string,
	sections []*dataobj.Section,
	remap []rankedSortKey,
	streamIDs []int64,
	lb *logs.Builder,
) ([]*encodedSection, error) {
	iter, err := mergeAndRemapLogsIter(ctx, sections, remap, streamIDs)
	if err != nil {
		return nil, fmt.Errorf("creating sort iterator: %w", err)
	}

	recorder := sectionRecorder{typ: lb.Type()}
	for rec := range iter {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		val, err := rec.Value()
		if err != nil {
			return nil, err
		}
		lb.Append(val)
		if lb.UncompressedSize() > int(b.cfg.TargetSectionSize) {
			if _, err := lb.Flush(&recorder); err != nil {
				return nil, err
			}
			lb.Reset()
			lb.SetTenant(tenant)
		}
	}
	if _, err := lb.Flush(&recorder); err != nil {
		return nil, err
	}
	lb.Reset()
	lb.SetTenant(tenant)
	return recorder.sections, nil
}

// sectionRecorder is a [dataobj.SectionWriter] that keeps a copy of each
// section written to it, so that the section can reach a [dataobj.Builder]
// later.
type sectionRecorder struct {
	typ      dataobj.SectionType
	sections []*encodedSection
}

func (r *sectionRecorder) WriteSection(opts *dataobj.WriteSectionOptions, data, metadata []byte) (int64, error) {
	// The writer must not keep the slices that it receives, so copy them.
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
