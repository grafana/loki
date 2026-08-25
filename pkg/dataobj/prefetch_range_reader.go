package dataobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

// prefetchedRangeReader wraps a rangeReader with a prefetched byte window.
// Overlapping ranges are served partially or wholly from prefetched bytes.
type prefetchedRangeReader struct {
	inner rangeReader

	prefetchOffset int64
	prefetched     []byte
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

	// intersectStart/intersectEnd describe the intersection of:
	//   - requested range [offset, requestedEnd)
	//   - prefetched range [prefetchedStart, prefetchedEnd)
	// The intersection can be empty.
	var (
		prefetchedStart = rr.prefetchOffset
		prefetchedEnd   = rr.prefetchOffset + int64(len(rr.prefetched))

		intersectStart = max(offset, prefetchedStart)
		intersectEnd   = min(requestedEnd, prefetchedEnd)
	)

	if intersectStart >= intersectEnd {
		return rr.inner.ReadRange(ctx, offset, length)
	}

	var (
		readers []io.Reader
		closers []io.Closer
	)

	// Callers only set a prefetched byte range that is at the absolute head or
	// absolute tail of the object (never in the middle), so there will only be
	// up to 2 readers.
	//
	// However, we handle the middle case anyway to be defensive.

	if offset < intersectStart {
		prefix, err := rr.inner.ReadRange(ctx, offset, intersectStart-offset)
		if err != nil {
			return nil, err
		}
		readers = append(readers, prefix)
		closers = append(closers, prefix)
	}

	prefetchChunk := rr.prefetched[intersectStart-prefetchedStart : intersectEnd-prefetchedStart]
	if len(prefetchChunk) > 0 {
		readers = append(readers, bytes.NewReader(prefetchChunk))
	}

	if intersectEnd < requestedEnd {
		suffix, err := rr.inner.ReadRange(ctx, intersectEnd, requestedEnd-intersectEnd)
		if err != nil {
			closeErr := closeClosers(closers)
			if closeErr != nil {
				return nil, errors.Join(err, closeErr)
			}
			return nil, err
		}
		readers = append(readers, suffix)
		closers = append(closers, suffix)
	}

	return &multiReadCloser{
		reader:  io.MultiReader(readers...),
		closers: closers,
	}, nil
}

type multiReadCloser struct {
	reader  io.Reader
	closers []io.Closer
}

func (rc *multiReadCloser) Read(p []byte) (int, error) {
	return rc.reader.Read(p)
}

func (rc *multiReadCloser) Close() error {
	return closeClosers(rc.closers)
}

func closeClosers(closers []io.Closer) error {
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
