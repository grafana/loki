package dataobj

import (
	"bytes"
	"context"
	"io"
	"testing"

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

	t.Run("Read reports only the unread bytes after a partial read", func(t *testing.T) {
		rc, err := rr.Read(context.Background())
		require.NoError(t, err)

		_, err = io.ReadFull(rc, make([]byte, 10))
		require.NoError(t, err)

		size, err := objstore.TryToGetSize(rc)
		require.NoError(t, err)
		require.Equal(t, int64(len(source)-10), size)
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
