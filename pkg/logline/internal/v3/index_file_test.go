package v3

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

type openTrackingReaderAt struct {
	reader  io.ReaderAt
	offsets []int64
	lengths []int
}

func (r *openTrackingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.offsets = append(r.offsets, off)
	r.lengths = append(r.lengths, len(p))
	return r.reader.ReadAt(p, off)
}

// writeTestIndex creates a LOGL index file with the given documents and term→bitmap
// postings. Returns the file path.
func writeTestIndex(t *testing.T, dir, name string, docs []format.DocumentMetadata, postings map[[8]byte][]uint32) string {
	t.Helper()
	path := filepath.Join(dir, name)
	cfg := DefaultFastIndexWriteConfig()
	cfg.DensityThreshold = 0 // disable density filter in test helpers

	// Sort terms for StreamingIndexWriter (requires ascending order).
	type kv struct {
		term [8]byte
		ids  []uint32
	}
	sorted := make([]kv, 0, len(postings))
	for term, ids := range postings {
		sorted = append(sorted, kv{term, ids})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return compareTerm8(sorted[i].term, sorted[j].term) < 0
	})

	w, err := newStreamingIndexWriter(path, cfg, uint32(len(docs)))
	require.NoError(t, err)
	w.AddDocuments(docs)
	bm := roaring.New()
	for _, entry := range sorted {
		bm.Clear()
		for _, id := range entry.ids {
			bm.Add(id)
		}
		require.NoError(t, w.WriteTermBitmap(entry.term, format.Bitmap{Roaring: bm}))
	}
	require.NoError(t, w.Close())
	return path
}

func term(s string) [8]byte {
	var t [8]byte
	copy(t[:], s)
	return t
}

func TestReadIndexHeader(t *testing.T) {
	dir := t.TempDir()
	path := writeTestIndex(t, dir, "header.idx",
		[]format.DocumentMetadata{
			{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
			{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
		},
		map[[8]byte][]uint32{
			term("TERMAA"): {0, 1},
			term("TERMBB"): {1},
		},
	)

	h, err := ReadIndexHeader(path)
	require.NoError(t, err)
	require.Equal(t, IndexMagic, h.Magic)
	require.Equal(t, IndexVersion, h.Version)
	require.Equal(t, uint32(2), h.DocumentCount)
	require.Equal(t, uint64(2), h.TermCount)
	require.True(t, h.PostingsDataSize > 0)
	require.True(t, h.TermDataSize > 0)
	require.True(t, h.DocMetadataSize > 0)
}

func TestReadIndexHeader_NotAnIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.idx")
	garbage := make([]byte, IndexFooterSize)
	copy(garbage, "not a real index file")
	require.NoError(t, os.WriteFile(path, garbage, 0o644))

	_, err := ReadIndexHeader(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid index magic")
}

func TestReadIndexHeader_TooSmall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tiny.idx")
	require.NoError(t, os.WriteFile(path, []byte("tiny"), 0o644))

	_, err := ReadIndexHeader(path)
	require.Error(t, err)
}

func TestOpenIndexAtWithHeader_SkipsHeaderRead(t *testing.T) {
	dir := t.TempDir()
	path := writeTestIndex(t, dir, "header-skip.idx",
		[]format.DocumentMetadata{
			{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
			{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
		},
		map[[8]byte][]uint32{
			term("TERMAA"): {0, 1},
			term("TERMBB"): {1},
		},
	)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	header, err := ReadIndexHeader(path)
	require.NoError(t, err)

	tracker := &openTrackingReaderAt{reader: bytes.NewReader(raw)}
	reader, err := OpenIndexAtWithHeader(tracker, 0, int64(len(raw)), header.Info())
	require.NoError(t, err)
	defer reader.Close()

	require.Len(t, tracker.offsets, 1, "expected a single metadata-region read during open")
	metadataOffset := int64(header.PostingsDataSize) + int64(header.TermDataSize)
	metadataSize := int(header.DocMetadataSize + header.TermBlockDirSize + header.PostingsBlockDirSize)
	require.Equal(t, metadataOffset, tracker.offsets[0], "open should skip header read at offset 0")
	require.Equal(t, metadataSize, tracker.lengths[0], "open should read exactly the metadata region")
}

func TestOpenIndexAtWithMetadata_SkipsOpenReads(t *testing.T) {
	dir := t.TempDir()
	path := writeTestIndex(t, dir, "metadata-cache.idx",
		[]format.DocumentMetadata{
			{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
			{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
		},
		map[[8]byte][]uint32{
			term("TERMAA"): {0, 1},
			term("TERMBB"): {1},
		},
	)

	seedReader, err := OpenIndexFile(path)
	require.NoError(t, err)
	header := seedReader.Header()
	metadata := seedReader.Metadata()
	require.NotNil(t, metadata)
	require.NoError(t, seedReader.Close())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	tracker := &openTrackingReaderAt{reader: bytes.NewReader(raw)}

	reader, err := OpenIndexAtWithMetadata(tracker, 0, int64(len(raw)), header, metadata)
	require.NoError(t, err)
	defer reader.Close()

	require.Empty(t, tracker.offsets, "opening with cached metadata should not issue range reads")

	// Query still works and triggers normal block reads after open.
	docIDs, err := reader.query("TERMAA")
	require.NoError(t, err)
	require.NotEmpty(t, docIDs)
}

func TestTermIterator_EmptyIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.idx")
	writer, err := newStreamingIndexWriter(path, DefaultFastIndexWriteConfig(), 0)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)
	require.False(t, it.Next())
	require.NoError(t, it.Err())
}

func TestNewTermIterator_RejectsZeroTermBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.idx")

	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	require.NoError(t, err)
	compressed := encoder.EncodeAll(nil, nil)
	require.NoError(t, encoder.Close())

	termData := make([]byte, 4+len(compressed))
	binary.LittleEndian.PutUint32(termData[:4], uint32(len(compressed)))
	copy(termData[4:], compressed)

	termDir, err := encodeTermBlockDirEntries([]termBlockDirEntry{{
		BlockOffset:    0,
		CompressedSize: uint32(len(termData)),
		TermCount:      0,
	}})
	require.NoError(t, err)

	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()

	// v2: data first, footer at end.
	_, err = file.Write(termData)
	require.NoError(t, err)
	_, err = file.Write(termDir)
	require.NoError(t, err)
	require.NoError(t, writeIndexHeader(file, IndexFooter{
		Magic:            IndexMagic,
		Version:          IndexVersion,
		Flags:            composeIndexFlags(format.PostingsEncodingFastDeltaVarIntBlocked, 1),
		TermBlockCount:   1,
		TermDataSize:     uint64(len(termData)),
		TermBlockDirSize: uint64(len(termDir)),
	}))
	require.NoError(t, file.Close())

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	_, err = r.NewTermIterator()
	require.Error(t, err)
	require.Contains(t, err.Error(), "zero terms")
}
