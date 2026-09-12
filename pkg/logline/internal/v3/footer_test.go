package v3

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestFooterLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lidx")

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0 // disable density filter for this test
	w, err := newStreamingIndexWriter(path, cfg, 3)
	require.NoError(t, err)
	w.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 0, MaxTimeUnix: 1},
		{ID: 1, MinTimeUnix: 1, MaxTimeUnix: 2},
		{ID: 2, MinTimeUnix: 2, MaxTimeUnix: 3},
	})

	bm := roaring.New()
	bm.Add(0)
	require.NoError(t, w.WriteTermBitmap([8]byte{'a', 'b', 'c', 'd', 'e', 'f', 0, 0}, format.Bitmap{Roaring: bm}))
	require.NoError(t, w.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	info, err := f.Stat()
	require.NoError(t, err)
	size := info.Size()

	// First 4 bytes must NOT be IndexMagic (no header prefix; footer at EOF).
	var first4 [4]byte
	_, err = f.ReadAt(first4[:], 0)
	require.NoError(t, err)
	require.NotEqual(t, IndexMagic, binary.LittleEndian.Uint32(first4[:]),
		"index must not have magic at offset 0")

	// Last 256 bytes must be a valid footer with the current on-disk version.
	footerBuf := make([]byte, IndexFooterSize)
	_, err = f.ReadAt(footerBuf, size-IndexFooterSize)
	require.NoError(t, err)
	h, err := parseIndexHeader(footerBuf)
	require.NoError(t, err)
	require.Equal(t, IndexMagic, h.Magic, "footer must have valid magic")
	require.Equal(t, IndexVersion, h.Version, "footer version must match IndexVersion")
	require.Equal(t, uint64(1), h.TermCount)
	require.Equal(t, uint32(3), h.DocumentCount)
}

func TestFooterReadback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lidx")

	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0
	w, err := newStreamingIndexWriter(path, cfg, 2)
	require.NoError(t, err)
	w.AddDocuments([]format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
		{ID: 1, MinTimeUnix: 200, MaxTimeUnix: 300},
	})

	bm0 := roaring.New()
	bm0.Add(0)
	bm1 := roaring.New()
	bm1.AddMany([]uint32{0, 1})
	require.NoError(t, w.WriteTermBitmap([8]byte{'a', 'b', 'c', 'd', 'e', 'f', 0, 0}, format.Bitmap{Roaring: bm0}))
	require.NoError(t, w.WriteTermBitmap([8]byte{'b', 'b', 'b', 'b', 'b', 'b', 0, 0}, format.Bitmap{Roaring: bm1}))
	require.NoError(t, w.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()
	require.Equal(t, uint64(2), r.header.TermCount)
	require.Equal(t, uint32(2), r.header.DocumentCount)
	require.Equal(t, IndexVersion, r.header.Version)
}
