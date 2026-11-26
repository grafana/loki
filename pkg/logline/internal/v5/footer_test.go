package v5

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestFooterLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lidx")

	cfg := testWriteConfig()
	cfg.DensityThreshold = 0 // disable density filter for this test
	w, err := newStreamingIndexWriter(path, cfg)
	require.NoError(t, err)

	bm := roaring.New()
	bm.AddMany([]uint32{0, 7})
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
	require.Equal(t, uint32(5), binary.LittleEndian.Uint32(footerBuf[4:8]), "v5 footer version is 5")
	require.Equal(t, uint64(1), h.TermCount)
	require.Equal(t, uint32(2), h.DocumentCount, "DocumentCount counts the cells postings reference")
	require.Zero(t, h.DocMetadataSize, "v5 indexes have no document table")

	// ReservedMid: interval (bytes 0-8), shard bits (8), day start (16-24).
	mid := footerBuf[32:128]
	require.Equal(t, uint64(time.Second), binary.LittleEndian.Uint64(mid[0:8]))
	require.Equal(t, byte(0), mid[8])
	require.Equal(t, uint64(testDayStart.UnixNano()), binary.LittleEndian.Uint64(mid[16:24]))
}

func TestFooterReadback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lidx")

	cfg := testWriteConfig()
	cfg.DensityThreshold = 0
	w, err := newStreamingIndexWriter(path, cfg)
	require.NoError(t, err)

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
