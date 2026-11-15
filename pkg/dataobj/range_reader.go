package dataobj

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/thanos-io/objstore"
)

// rangeReader is an interface that can read a range of bytes from an object.
type rangeReader interface {
	// Size returns the full size of the object.
	Size(ctx context.Context) (int64, error)

	// Read returns a reader over the entire object. Callers may create multiple
	// concurrent instances of Read.
	Read(ctx context.Context) (io.ReadCloser, error)

	// ReadRange returns a reader over a range of bytes. Callers may create
	// multiple concurrent instances of ReadRange.
	ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error)
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
