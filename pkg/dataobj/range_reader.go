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

// nopCloserWithObjectSize reads a section of an object and reports how many
// bytes are left to read. Its Close does nothing.
//
// It implements [objstore.ObjectSizer], so an upload of the reader knows its
// size. Without the size, the S3 client starts a multipart upload, even for a
// small object, and allocates a part buffer of about 528MiB for each upload.
//
// nopCloserWithObjectSize does not expose [io.ReaderAt] or [io.Seeker]. If the
// reader has them, minio-go reads the upload through ReadAt, and the objstore
// upload metrics miss those bytes, because they count only what Read returns.
type nopCloserWithObjectSize struct {
	r *io.SectionReader
}

var _ objstore.ObjectSizer = nopCloserWithObjectSize{}

func (r nopCloserWithObjectSize) Read(p []byte) (int, error) { return r.r.Read(p) }

func (r nopCloserWithObjectSize) Close() error { return nil }

// ObjectSize returns the number of bytes left to read.
func (r nopCloserWithObjectSize) ObjectSize() (int64, error) {
	offset, err := r.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	return r.r.Size() - offset, nil
}
