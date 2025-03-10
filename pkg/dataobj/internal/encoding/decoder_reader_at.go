package encoding

import (
	"context"
	"fmt"
	"io"
	"math"
)

// ReaderAtDecoder decodes a data object from the provided [io.ReaderAt]. The
// size argument specifies the total number of bytes in r.
func ReaderAtDecoder(r io.ReaderAt, size int64) Decoder {
	return &rangeDecoder{
		r: &readerAtRangeReader{r: r, size: size},
	}
}

type readerAtRangeReader struct {
	size int64
	r    io.ReaderAt
}

func (rr *readerAtRangeReader) Size(_ context.Context) (int64, error) {
	return rr.size, nil
}

func (rr *readerAtRangeReader) ReadRange(_ context.Context, offset int64, length int64) (io.ReadCloser, error) {
	if length > math.MaxInt {
		return nil, fmt.Errorf("length too large: %d", length)
	}
	return io.NopCloser(io.NewSectionReader(rr.r, offset, length)), nil
}
