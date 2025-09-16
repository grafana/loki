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

func (rr *readerAtRangeReader) Size(_ context.Context) (int64, error) {
	return rr.size, nil
}

func (rr *readerAtRangeReader) Read(_ context.Context) (io.ReadCloser, error) {
	return io.NopCloser(io.NewSectionReader(rr.r, 0, rr.size)), nil
}

func (rr *readerAtRangeReader) ReadRange(_ context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if length > math.MaxInt {
		return nil, fmt.Errorf("length too large: %d", length)
	}
	return io.NopCloser(io.NewSectionReader(rr.r, offset, length)), nil
}
