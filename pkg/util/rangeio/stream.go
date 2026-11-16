package rangeio

import (
	"context"
	"errors"
	"io"
	"slices"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
)

// StreamOpener opens a stream over a range of an input source.
type StreamOpener interface {
	// OpenRange returns a reader of the length bytes that start at offset. The
	// caller closes it.
	//
	// When the range starts at or beyond the end of the input source, OpenRange
	// or the first Read of the reader must return an error that wraps [io.EOF].
	//
	// It is safe to call OpenRange concurrently from multiple goroutines.
	OpenRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)
}

// ReadRangesStream populates Data for each element in ranges, like
// [ReadRanges]. It coalesces, splits and reads the ranges in the same way,
// and it takes the same configuration from the context.
//
// Unlike [ReadRanges], it reads each coalesced range as one stream. It reads
// the bytes of each range straight into Data, and discards the bytes of the
// gaps between the ranges. It allocates no buffer for the coalesced range. A
// stream starts at the first byte that a range needs and ends at the last
// byte that a range needs, so the stream of a split range never reads a gap
// at its edges.
//
// Overlapping ranges are read once. ReadRangesStream copies the overlap from
// a range that it already filled.
//
// ReadRangesStream returns an error if a stream fails. It returns [io.EOF]
// only if a stream starts at or beyond the end of the input source. It returns
// [io.ErrUnexpectedEOF] if a stream ends after it gave some bytes but before
// it filled every range in it. When it returns an error, Data of some ranges can be
// half filled.
func ReadRangesStream(ctx context.Context, o StreamOpener, ranges []Range) error {
	startTime := time.Now()
	ctx, span := tracer.Start(ctx, "ReadRangesStream")
	defer span.End()

	cfg := configFromContext(ctx)
	if cfg == nil {
		cfg = &DefaultConfig
		span.SetAttributes(attribute.Bool("config.default", true))
	}

	chunks := planChunks(cfg, ranges)
	parts := assignParts(chunks, ranges)

	var chunksSize uint64
	for _, c := range chunks {
		chunksSize += uint64(c.Length)
	}
	span.SetAttributes(readRangesAttributes(cfg, ranges, len(chunks), chunksSize)...)
	defer injectThroughputAttribute(span, startTime, chunksSize)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.effectiveParallelism())

	var gotEOF atomic.Bool

	for i := range chunks {
		if len(parts[i]) == 0 {
			continue
		}
		g.Go(func() error {
			err := readParts(ctx, o, parts[i])
			if errors.Is(err, io.EOF) {
				// Like [ReadRanges], let the other streams finish and report the EOF at the end.
				gotEOF.Store(true)
				return nil
			}
			return err
		})
	}

	if err := g.Wait(); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "") // Even if we got [io.EOF], we treat the operation as successful here.

	if gotEOF.Load() {
		return io.EOF
	}
	return nil
}

// part is the bytes of one range that lie in one chunk.
type part struct {
	data   []byte
	offset int64 // The offset of data[0] in the input source.
}

func (p part) end() int64 { return p.offset + int64(len(p.data)) }

// assignParts returns, for each chunk, the parts of the ranges that lie in the chunk, sorted by
// offset. It skips empty ranges. A range that spans several chunks has one part in each of them.
// The parts of different chunks never share a byte of Data.
func assignParts(chunks []chunk, ranges []Range) [][]part {
	sorted := make([]Range, 0, len(ranges))
	for _, r := range ranges {
		if len(r.Data) > 0 {
			sorted = append(sorted, r)
		}
	}
	slices.SortStableFunc(sorted, func(a, b Range) int {
		switch {
		case a.Offset < b.Offset:
			return -1
		case a.Offset > b.Offset:
			return 1
		default:
			return 0
		}
	})

	parts := make([][]part, len(chunks))
	for _, r := range sorted {
		end := r.Offset + r.Len()
		i := sort.Search(len(chunks), func(i int) bool { return chunks[i].end() > r.Offset })
		for ; i < len(chunks) && chunks[i].Offset < end; i++ {
			start, stop := max(r.Offset, chunks[i].Offset), min(end, chunks[i].end())
			parts[i] = append(parts[i], part{data: r.Data[start-r.Offset : stop-r.Offset], offset: start})
		}
	}
	return parts
}

// readParts opens one stream that spans the parts, and fills the parts from it. The parts must be
// sorted by offset and not empty.
func readParts(ctx context.Context, o StreamOpener, parts []part) error {
	start := parts[0].offset
	end := start
	for _, p := range parts {
		end = max(end, p.end())
	}

	spanStart := time.Now()
	rc, err := o.OpenRange(ctx, start, end-start)
	if err != nil {
		return err
	}
	defer rc.Close()

	var (
		pos      = start // The offset of the next byte of the stream.
		furthest part    // The part that ends at pos. It holds the bytes of any overlap.
	)
	for _, p := range parts {
		whole := p
		if p.offset < pos {
			// Every earlier part starts at or before p. The furthest one ends at pos, so it holds
			// the first bytes of p.
			n := copy(p.data, furthest.data[p.offset-furthest.offset:])
			p = part{data: p.data[n:], offset: p.offset + int64(n)}
			if len(p.data) == 0 {
				continue
			}
		}

		if gap := p.offset - pos; gap > 0 {
			n, err := io.CopyN(io.Discard, rc, gap)
			pos += n
			if err != nil {
				return unexpectedEOF(err, pos > start)
			}
		}
		n, err := io.ReadFull(rc, p.data)
		pos += int64(n)
		if err != nil {
			return unexpectedEOF(err, pos > start)
		}
		furthest = whole
	}

	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent("read stream", trace.WithAttributes(
			attribute.Int64("offset", start),
			attribute.Int64("len", end-start),
			attribute.Int("parts", len(parts)),
			attribute.Stringer("read.duration", time.Since(spanStart)),
		))
	}
	return nil
}

// unexpectedEOF turns an end of the stream into [io.ErrUnexpectedEOF] when the stream gave bytes
// before. An end before the first byte stays [io.EOF]: the range lies beyond the end of the input
// source.
func unexpectedEOF(err error, gotBytes bool) error {
	if gotBytes && errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
