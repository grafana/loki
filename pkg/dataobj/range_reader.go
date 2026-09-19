package dataobj

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/xcap"
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
	rc, err := rr.bucket.Get(ctx, rr.path)
	if err != nil {
		return nil, err
	}
	return newDownloadCountingReadCloser(ctx, rc), nil
}

func (rr *bucketRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	rc, err := rr.bucket.GetRange(ctx, rr.path, offset, length)
	if err != nil {
		return nil, err
	}
	return newDownloadCountingReadCloser(ctx, rc), nil
}

// downloadCountingReadCloser wraps an [io.ReadCloser] returned by an
// object-store request and accumulates the number of bytes read from it.
// The accumulated total is recorded against [StatObjectBytesDownloaded] on
// [xcap.Region] in the context (if any) when the reader is closed, so the
// total reflects bytes actually transferred from storage rather than the
// requested range size.
type downloadCountingReadCloser struct {
	inner  io.ReadCloser
	region *xcap.Region
	n      int64
}

func newDownloadCountingReadCloser(ctx context.Context, rc io.ReadCloser) *downloadCountingReadCloser {
	return &downloadCountingReadCloser{
		inner:  rc,
		region: xcap.RegionFromContext(ctx),
	}
}

func (rc *downloadCountingReadCloser) Read(p []byte) (int, error) {
	n, err := rc.inner.Read(p)
	rc.n += int64(n)
	return n, err
}

func (rc *downloadCountingReadCloser) Close() error {
	if rc.n > 0 {
		rc.region.Record(StatObjectBytesDownloaded.Observe(rc.n))
		rc.n = 0
	}
	return rc.inner.Close()
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
	return io.NopCloser(io.NewSectionReader(rr.r, 0, rr.size)), nil
}

func (rr *readerAtRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	} else if length > math.MaxInt {
		return nil, fmt.Errorf("length too large: %d", length)
	}
	return io.NopCloser(io.NewSectionReader(rr.r, offset, length)), nil
}
