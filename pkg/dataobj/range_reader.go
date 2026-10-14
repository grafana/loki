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
	return sectionReadCloser{io.NewSectionReader(rr.r, 0, rr.size)}, nil
}

func (rr *readerAtRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	} else if length > math.MaxInt {
		return nil, fmt.Errorf("length too large: %d", length)
	}
	return sectionReadCloser{io.NewSectionReader(rr.r, offset, length)}, nil
}

// sectionReadCloser is an [io.SectionReader] whose Close does nothing.
//
// It implements [objstore.ObjectSizer], so an upload of the reader knows its
// size. Without the size, the S3 client starts a multipart upload, even for a
// small object, and allocates a part buffer of about 528MiB for each upload.
type sectionReadCloser struct {
	*io.SectionReader
}

var _ objstore.ObjectSizer = sectionReadCloser{}

func (r sectionReadCloser) Close() error { return nil }

// ObjectSize returns the number of bytes left to read.
func (r sectionReadCloser) ObjectSize() (int64, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	return r.Size() - offset, nil
}
