package dataobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

// prefetchedRangeReader wraps a rangeReader with prefetched ranges of bytes. ReadRange and
// ReadRanges serve the parts that fall inside a prefetched range from memory and read the gaps from
// the inner reader. Both read the gaps in parallel.
type prefetchedRangeReader struct {
	inner rangeReader

	// ranges are sorted by offset and do not overlap.
	ranges metadata.PrefetchedRanges
}

var _ rangeReader = (*prefetchedRangeReader)(nil)

func (rr *prefetchedRangeReader) Size(ctx context.Context) (int64, error) {
	return rr.inner.Size(ctx)
}

func (rr *prefetchedRangeReader) Read(ctx context.Context) (io.ReadCloser, error) {
	return rr.inner.Read(ctx)
}

func (rr *prefetchedRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if length < 0 {
		return nil, fmt.Errorf("length must not be negative: %d", length)
	} else if length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	requestedEnd := offset + length
	if requestedEnd < offset {
		return nil, fmt.Errorf("read range overflows int64: offset=%d length=%d", offset, length)
	}

	overlapping := rr.rangesOverlapping(offset, length)
	if len(overlapping) == 0 {
		return rr.inner.ReadRange(ctx, offset, length)
	}

	segments := rangeSegmentsFromPrefetchedRanges(overlapping, offset, requestedEnd)
	if err := rr.openRangeSegmentReaders(ctx, segments); err != nil {
		return nil, err
	}

	return newRangeSegmentsReader(segments), nil
}

// ReadRanges returns the bytes of each requested range, in the order of ranges.
//
// When one prefetched range holds all the bytes of a requested range, the result aliases that
// prefetched range. The capacity of that result equals its length.
func (rr *prefetchedRangeReader) ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, error) {
	var (
		results = make([][]byte, len(ranges))
		gaps    []rangeio.Range
	)
	for i, r := range ranges {
		if err := validateAbsoluteByteRange(r); err != nil {
			return nil, err
		}

		// Return a slice of the prefetched range when one range holds all the bytes. This saves an
		// allocation and a copy.
		if data, ok := rr.zeroCopyPrefetchedRange(r.Offset, r.Length); ok {
			results[i] = data
			continue
		}

		// Allocate a new slice for the bytes. The call below copies the prefetched bytes into it and
		// adds a read for each part that is not prefetched.
		results[i] = make([]byte, r.Length)
		gaps = rr.copyPrefetchedRangeAndCollectGaps(gaps, results[i], r.Offset)
	}

	// Give rangeio only the gaps, so a call with every byte prefetched reads nothing. rangeio can still
	// merge two close gaps into one read, to save a request. That read also covers the bytes between
	// the gaps, even when they are prefetched.
	if err := readIORanges(ctx, rr.inner, gaps); err != nil {
		return nil, err
	}
	return results, nil
}

// openRangeSegmentReaders opens a reader on the inner reader for every segment without data, all at
// the same time. When one fails, it waits for the others, closes every reader that it opened and
// returns the first error.
func (rr *prefetchedRangeReader) openRangeSegmentReaders(ctx context.Context, segments rangeSegments) error {
	// The group must not use errgroup.WithContext. That context ends when Wait returns, and it
	// would cancel the readers that this method hands back.
	var g errgroup.Group
	for i := range segments {
		s := &segments[i]
		if s.data != nil {
			continue
		}
		g.Go(func() (err error) {
			s.reader, err = rr.inner.ReadRange(ctx, s.readerOffset, s.readerLength)
			return err
		})
	}

	err := g.Wait()
	if err == nil {
		return nil
	}

	if closeErr := segments.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

// copyPrefetchedRangeAndCollectGaps copies the prefetched bytes of [offset, offset+len(buf)) into
// buf. It returns gaps with one rangeio.Range appended for each part of buf that is not prefetched.
// Each appended range reads into the matching part of buf.
func (rr *prefetchedRangeReader) copyPrefetchedRangeAndCollectGaps(gaps []rangeio.Range, buf []byte, offset int64) []rangeio.Range {
	if len(buf) == 0 {
		return gaps
	}

	overlapping := rr.rangesOverlapping(offset, int64(len(buf)))
	if len(overlapping) == 0 {
		return append(gaps, rangeio.Range{Data: buf, Offset: offset})
	}

	pos := 0
	for _, s := range rangeSegmentsFromPrefetchedRanges(overlapping, offset, offset+int64(len(buf))) {
		if s.data != nil {
			pos += copy(buf[pos:], s.data)
			continue
		}
		next := pos + int(s.readerLength)
		gaps = append(gaps, rangeio.Range{Data: buf[pos:next], Offset: s.readerOffset})
		pos = next
	}
	return gaps
}

// zeroCopyPrefetchedRange returns the bytes of [offset, offset+length) when one prefetched range
// holds all of them. It returns false when length is zero or less, or when offset+length overflows.
//
// It also returns false when the bytes span more than one prefetched range, even when the ranges
// touch. A slice that joins them needs a copy.
//
// The slice aliases the prefetched range, so the caller must not modify it. Its capacity equals its
// length, so an append does not write into the bytes that follow it in the range.
func (rr *prefetchedRangeReader) zeroCopyPrefetchedRange(offset, length int64) ([]byte, bool) {
	end := offset + length
	if length <= 0 || end < offset {
		return nil, false
	}

	overlapping := rr.rangesOverlapping(offset, length)
	if len(overlapping) != 1 {
		return nil, false
	}
	r := overlapping[0]
	if r.Offset > offset || end > r.Offset+int64(len(r.Data)) {
		return nil, false
	}
	return r.Data[offset-r.Offset : end-r.Offset : end-r.Offset], true
}

// rangesOverlapping returns the prefetched ranges that share at least one byte with
// [offset, offset+length), in order. The slice aliases rr.ranges. The caller must pass a positive
// length and make sure that offset+length does not overflow.
func (rr *prefetchedRangeReader) rangesOverlapping(offset, length int64) metadata.PrefetchedRanges {
	end := offset + length

	// Search for the start, because a scan would visit every range before offset. The end needs no
	// search: the loop visits only overlapping ranges, and the caller visits them next anyway.
	lo := sort.Search(len(rr.ranges), func(i int) bool {
		return rr.ranges[i].Offset+int64(len(rr.ranges[i].Data)) > offset
	})
	hi := lo
	for hi < len(rr.ranges) && rr.ranges[hi].Offset < end {
		hi++
	}
	return rr.ranges[lo:hi]
}

// rangeSegmentsReader reads the bytes of rangeSegments in order.
type rangeSegmentsReader struct {
	reader   io.Reader
	segments rangeSegments
}

// newRangeSegmentsReader returns a reader over the bytes of segments. Each segment without data must
// have an open reader. The returned reader owns those readers.
func newRangeSegmentsReader(segments rangeSegments) *rangeSegmentsReader {
	readers := make([]io.Reader, len(segments))
	for i, s := range segments {
		if s.reader == nil {
			readers[i] = bytes.NewReader(s.data)
		} else {
			readers[i] = s.reader
		}
	}
	return &rangeSegmentsReader{
		reader:   io.MultiReader(readers...),
		segments: segments,
	}
}

func (rc *rangeSegmentsReader) Read(p []byte) (int, error) {
	return rc.reader.Read(p)
}

// Close closes the readers of the segments.
func (rc *rangeSegmentsReader) Close() error {
	return rc.segments.Close()
}

// rangeSegment is a part of a requested range. It holds either prefetched bytes or a gap that the
// inner reader serves.
type rangeSegment struct {
	// data holds the prefetched bytes of the segment. It is nil for a gap.
	data []byte

	// readerOffset and readerLength give the range of a gap in the object. The inner reader serves it.
	readerOffset, readerLength int64

	// reader is the reader that openRangeSegmentReaders opened for a gap.
	reader io.ReadCloser
}

// rangeSegments are the segments of one requested range, in order.
type rangeSegments []rangeSegment

// Close closes the readers of the segments, last first, and returns their errors joined. It skips
// the segments that have no reader.
func (ss rangeSegments) Close() error {
	var errs []error
	for i := len(ss) - 1; i >= 0; i-- {
		if ss[i].reader == nil {
			continue
		}
		if err := ss[i].reader.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// rangeSegmentsFromPrefetchedRanges cuts [offset, end) into segments, in order. The segments are the
// prefetched bytes of ranges and the gaps between them. It does not read the gaps.
//
// Each range must overlap [offset, end). The ranges must be sorted by offset and must not overlap
// each other.
func rangeSegmentsFromPrefetchedRanges(ranges metadata.PrefetchedRanges, offset, end int64) rangeSegments {
	segments := make(rangeSegments, 0, 2*len(ranges)+1)

	pos := offset
	for _, r := range ranges {
		if r.Offset > pos {
			segments = append(segments, rangeSegment{readerOffset: pos, readerLength: r.Offset - pos})
			pos = r.Offset
		}

		chunkEnd := min(end, r.Offset+int64(len(r.Data)))
		if chunk := r.Data[pos-r.Offset : chunkEnd-r.Offset]; len(chunk) > 0 {
			segments = append(segments, rangeSegment{data: chunk})
		}
		pos = chunkEnd
	}
	if pos < end {
		segments = append(segments, rangeSegment{readerOffset: pos, readerLength: end - pos})
	}
	return segments
}
