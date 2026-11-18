package dataobj

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

// rangeReader reads ranges of bytes from an object.
type rangeReader interface {
	// Size returns the full size of the object.
	Size(ctx context.Context) (int64, error)

	// Read returns a reader over the entire object. Callers may create multiple
	// concurrent instances of Read.
	Read(ctx context.Context) (io.ReadCloser, error)

	// ReadRange returns a reader over a range of bytes. Callers may create
	// multiple concurrent instances of ReadRange.
	ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error)

	// ReadRanges returns the bytes of each range, and where the bytes came from.
	// The result has one slice for each range, in the order of ranges. It
	// returns an error if a range has a negative offset or length, or an end
	// that overflows an int64.
	//
	// A returned slice can alias memory that the reader shares. The caller must
	// not modify it.
	//
	// When ReadRanges returns an error, the readRangesStats count the requests that the
	// call sent and no bytes.
	//
	// ReadRanges returns the same bytes as ReadRange. A reader that embeds
	// another reader and overrides ReadRange must override ReadRanges too. The
	// promoted ReadRanges would not call the new ReadRange.
	ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, readRangesStats, error)
}

// readRangesStats describes where the bytes of one [rangeReader.ReadRanges] call came from.
type readRangesStats struct {
	// memoryBytes is the number of requested bytes that a prefetched range held, or that a reader over
	// an [io.ReaderAt] returned.
	memoryBytes int64

	// storageBytes is the number of requested bytes that no prefetched range held, so the reader read
	// them from the object store. It counts requested bytes, not transferred bytes: the reader can
	// merge two close reads into one read that also covers the bytes between them.
	storageBytes int64

	// storageRequests is the number of requests that the reader sent to the object store.
	storageRequests int64
}

// absoluteByteRange is a range of bytes in an object. Its Offset counts from the
// start of the object.
type absoluteByteRange RelativeByteRange

// validateAbsoluteByteRange returns an error if r has a negative offset or length,
// or if its end does not fit in an int64.
func validateAbsoluteByteRange(r absoluteByteRange) error {
	if r.Offset < 0 || r.Length < 0 {
		return fmt.Errorf("parameters must not be negative: offset=%d length=%d", r.Offset, r.Length)
	}
	if r.Offset+r.Length < r.Offset {
		return fmt.Errorf("range overflows int64: offset=%d length=%d", r.Offset, r.Length)
	}
	return nil
}

// readRanges reads each range from rr into a new buffer. It counts every byte as read from storage.
func readRanges(ctx context.Context, rr rangeReader, ranges []absoluteByteRange) ([][]byte, readRangesStats, error) {
	var (
		results      = make([][]byte, len(ranges))
		toRead       = make([]rangeio.Range, 0, len(ranges))
		storageBytes int64
	)

	for i, r := range ranges {
		if err := validateAbsoluteByteRange(r); err != nil {
			return nil, readRangesStats{}, err
		}
		results[i] = make([]byte, r.Length)

		// Leave a range of zero length out of the read. rangeio merges close ranges, so it would
		// stretch the read of a range near it.
		if r.Length > 0 {
			toRead = append(toRead, rangeio.Range{Data: results[i], Offset: r.Offset})
			storageBytes += r.Length
		}
	}

	stats, err := readIORanges(ctx, rr, toRead)
	if err != nil {
		return nil, readRangesStats{storageRequests: int64(stats.Requests)}, err
	}
	return results, readRangesStats{storageBytes: storageBytes, storageRequests: int64(stats.Requests)}, nil
}

// readIORanges reads each range from rr into the Data of the range, with one rangeio call. It is
// readRanges for rangeio ranges: the caller owns the buffers. rangeio can merge close ranges, so a
// read can cover bytes that no range holds.
func readIORanges(ctx context.Context, rr rangeReader, ranges []rangeio.Range) (rangeio.Stats, error) {
	// Skip rangeio for an empty slice. It would start an empty trace span.
	if len(ranges) == 0 {
		return rangeio.Stats{}, nil
	}

	return rangeio.ReadRanges(ctx, rangeIOReader{rr: rr}, ranges)
}

// rangeIOReader adapts a rangeReader to a [rangeio.Reader].
type rangeIOReader struct {
	rr rangeReader
}

var _ rangeio.Reader = rangeIOReader{}

func (r rangeIOReader) ReadRange(ctx context.Context, rg rangeio.Range) (n int, err error) {
	rc, err := r.rr.ReadRange(ctx, rg.Offset, rg.Len())
	if err != nil {
		return 0, err
	}
	// After a full read, rangeio ignores the error that this function returns, so a Close error has
	// no effect. After a short read, CloseAndHandleError keeps the read error.
	defer util.CloseAndHandleError(rc, &err)

	return io.ReadFull(rc, rg.Data)
}

// bucketRangeReader reads an object from a bucket. It records no statistics.
// [FromBucket] gives it an instrumented bucket, which does.
type bucketRangeReader struct {
	bucket objstore.BucketReader
	path   string
}

func (rr *bucketRangeReader) Size(ctx context.Context) (int64, error) {
	attrs, err := rr.bucket.Attributes(ctx, rr.path)
	if err != nil {
		return 0, fmt.Errorf("reading attributes: %w", err)
	}
	return attrs.Size, nil
}

func (rr *bucketRangeReader) Read(ctx context.Context) (io.ReadCloser, error) {
	return rr.bucket.Get(ctx, rr.path)
}

func (rr *bucketRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	return rr.bucket.GetRange(ctx, rr.path, offset, length)
}

func (rr *bucketRangeReader) ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, readRangesStats, error) {
	return readRanges(ctx, rr, ranges)
}

type readerAtRangeReader struct {
	size int64
	r    io.ReaderAt
}

func (rr *readerAtRangeReader) Size(ctx context.Context) (int64, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return rr.size, nil
}

func (rr *readerAtRangeReader) Read(ctx context.Context) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nopCloserWithObjectSize{io.NewSectionReader(rr.r, 0, rr.size)}, nil
}

func (rr *readerAtRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	} else if length > math.MaxInt {
		return nil, fmt.Errorf("length too large: %d", length)
	}
	return nopCloserWithObjectSize{io.NewSectionReader(rr.r, offset, length)}, nil
}

func (rr *readerAtRangeReader) ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, readRangesStats, error) {
	results, stats, err := readRanges(ctx, rr, ranges)

	// The reader sends no request to the object store, so its bytes are not storage bytes.
	return results, readRangesStats{memoryBytes: stats.storageBytes}, err
}

// nopCloserWithObjectSize reads a section of an object and reports the size
// of the section. Its Close does nothing.
//
// It implements [objstore.ObjectSizer], so an upload of the reader knows its
// size. Without the size, the S3 client starts a multipart upload, even for a
// small object, and allocates a part buffer of about 528MiB for each upload.
//
// It implements [io.Seeker], so the S3 client can rewind the reader and retry
// a failed request. The S3 client seeks to the start of the section before
// each attempt. So ObjectSize returns the size of the whole section, not the
// number of unread bytes.
//
// It does not implement [io.ReaderAt]. If the reader has it, the S3 client
// reads the upload through ReadAt, and the objstore upload metrics miss those
// bytes, because they count only what Read returns.
//
// It is not safe for concurrent use.
type nopCloserWithObjectSize struct {
	r *io.SectionReader
}

var _ objstore.ObjectSizer = nopCloserWithObjectSize{}

func (r nopCloserWithObjectSize) Read(p []byte) (int, error) { return r.r.Read(p) }

func (r nopCloserWithObjectSize) Seek(offset int64, whence int) (int64, error) {
	return r.r.Seek(offset, whence)
}

func (r nopCloserWithObjectSize) Close() error { return nil }

// ObjectSize returns the size of the whole section, whatever the read offset.
func (r nopCloserWithObjectSize) ObjectSize() (int64, error) { return r.r.Size(), nil }
