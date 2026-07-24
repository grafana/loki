package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileByteSlice(t *testing.T) {
	// Craft a payload larger than a page so residency handling spans multiple
	// pages and the last page is only partially backed by the file.
	pageSize := os.Getpagesize()
	size := pageSize*2 + 123
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i)
	}

	path := filepath.Join(t.TempDir(), "data.bin")
	require.NoError(t, os.WriteFile(path, payload, 0o644))

	bs, err := openFileByteSlice(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bs.Close()) })

	require.Equal(t, size, bs.Len())

	// Reads across page boundaries and near EOF must return exact bytes,
	// regardless of whether they are served from the mapping or via pread.
	cases := []struct{ start, end int }{
		{0, 4},
		{0, size},
		{pageSize - 10, pageSize + 10},
		{pageSize * 2, size},
		{size - 1, size},
	}
	for _, c := range cases {
		require.Equal(t, payload[c.start:c.end], bs.Range(c.start, c.end), "range [%d,%d)", c.start, c.end)
	}

	// A Sub view offsets all further reads.
	sub := bs.Sub(pageSize, size)
	require.Equal(t, size-pageSize, sub.Len())
	require.Equal(t, payload[pageSize:pageSize+16], sub.Range(0, 16))
}

// TestFileByteSlicePreadFallback forces the pread path by clearing the mapping
// and verifies reads still return correct data.
func TestFileByteSlicePreadFallback(t *testing.T) {
	payload := []byte("residency-aware index reader")
	path := filepath.Join(t.TempDir(), "data.bin")
	require.NoError(t, os.WriteFile(path, payload, 0o644))

	bs, err := openFileByteSlice(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bs.Close()) })

	// Drop the mapping so Range must use ReadAt.
	if len(bs.data) != 0 {
		require.NoError(t, munmapFile(bs.data))
	}
	bs.data = nil

	require.Equal(t, payload[0:9], bs.Range(0, 9))
	require.Equal(t, payload, bs.Range(0, len(payload)))
}
