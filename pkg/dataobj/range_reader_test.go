package dataobj

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func Test_readerAtRangeReader_ObjectSize(t *testing.T) {
	const source = "0123456789abcdefghijklmnopqrstuvwxyz"

	rr := &readerAtRangeReader{
		size: int64(len(source)),
		r:    bytes.NewReader([]byte(source)),
	}

	t.Run("Read reports the object size to an upload", func(t *testing.T) {
		rc, err := rr.Read(context.Background())
		require.NoError(t, err)

		size, err := objstore.TryToGetSize(rc)
		require.NoError(t, err)
		require.Equal(t, int64(len(source)), size)
		require.Equal(t, source, string(readAllAndClose(t, rc)))
	})

	t.Run("Read reports the whole object size after a partial read", func(t *testing.T) {
		rc, err := rr.Read(context.Background())
		require.NoError(t, err)

		_, err = io.ReadFull(rc, make([]byte, 10))
		require.NoError(t, err)

		size, err := objstore.TryToGetSize(rc)
		require.NoError(t, err)
		require.Equal(t, int64(len(source)), size)
		require.Equal(t, source[10:], string(readAllAndClose(t, rc)))
	})

	t.Run("ReadRange reports the range length to an upload", func(t *testing.T) {
		rc, err := rr.ReadRange(context.Background(), 4, 8)
		require.NoError(t, err)

		size, err := objstore.TryToGetSize(rc)
		require.NoError(t, err)
		require.Equal(t, int64(8), size)
		require.Equal(t, source[4:12], string(readAllAndClose(t, rc)))
	})
}

func Test_readerAtRangeReader_ReadRanges(t *testing.T) {
	newReader := func() rangeReader {
		return &readerAtRangeReader{size: int64(len(backendSource)), r: bytes.NewReader([]byte(backendSource))}
	}

	t.Run("returns the bytes of each range in the order of the ranges", func(t *testing.T) {
		got, err := newReader().ReadRanges(t.Context(), []absoluteByteRange{{Offset: 2, Length: 4}, {Offset: 0, Length: 2}})

		require.NoError(t, err)
		require.Equal(t, []string{"2345", "01"}, toStrings(got))
	})

	t.Run("returns every byte of a range when the reader returns one byte at a time", func(t *testing.T) {
		got, err := oneByteRangeReader{newReader()}.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 2, Length: 4}})

		require.NoError(t, err)
		require.Equal(t, []string{"2345"}, toStrings(got))
	})

	t.Run("does not read the bytes up to a range of zero length", func(t *testing.T) {
		var reads recordedReads

		got, err := reads.hook(newReader()).ReadRanges(t.Context(), []absoluteByteRange{{Offset: 0, Length: 2}, {Offset: 3, Length: 0}})

		require.NoError(t, err)
		require.Equal(t, []string{"01", ""}, toStrings(got))
		require.Equal(t, []readCall{{offset: 0, length: 2}}, reads.calls())
	})

	t.Run("returns an error for a range with a negative length", func(t *testing.T) {
		got, err := newReader().ReadRanges(t.Context(), []absoluteByteRange{{Offset: 0, Length: -1}})

		require.EqualError(t, err, "parameters must not be negative: offset=0 length=-1")
		require.Nil(t, got)
	})
}

// oneByteRangeReader returns the bytes of every read one at a time.
type oneByteRangeReader struct {
	rangeReader
}

func (r oneByteRangeReader) ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	rc, err := r.rangeReader.ReadRange(ctx, offset, length)
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{iotest.OneByteReader(rc), rc}, nil
}

func (r oneByteRangeReader) ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, error) {
	return readRanges(ctx, r, ranges)
}
