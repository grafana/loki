package v5

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// encodingTestTerm is one term of a synthetic index. A nil ids with
// matchesAll set is a density-filter sentinel.
type encodingTestTerm struct {
	key        [8]byte
	ids        []uint32
	matchesAll bool
}

// buildEncodingTestTerms returns numTerms sorted terms over numDocs documents,
// skewed like production postings: most terms are tiny, a few are dense, and
// every 97th term is a sentinel.
func buildEncodingTestTerms(rng *rand.Rand, numDocs, numTerms int) []encodingTestTerm {
	terms := make([]encodingTestTerm, numTerms)
	for i := range terms {
		copy(terms[i].key[:], fmt.Sprintf("T%05d", i))
		if i%97 == 0 {
			terms[i].matchesAll = true
			continue
		}
		var n int
		switch r := rng.IntN(100); {
		case r < 77:
			n = 1 + rng.IntN(5)
		case r < 97:
			n = 6 + rng.IntN(200)
		default:
			n = numDocs / (2 + rng.IntN(8))
		}
		n = min(n, numDocs)
		set := make(map[uint32]struct{}, n)
		for len(set) < n {
			set[uint32(rng.IntN(numDocs))] = struct{}{}
		}
		ids := make([]uint32, 0, n)
		for id := range set {
			ids = append(ids, id)
		}
		terms[i].ids = uniqueSorted(ids)
	}
	return terms
}

func roaringOf(ids []uint32) *roaring.Bitmap {
	bm := roaring.New()
	bm.AddMany(ids)
	return bm
}

// writeEncodingTestIndex writes terms with cfg, alternating between the
// bitmap and docID write paths so both encoder entry points are covered.
func writeEncodingTestIndex(t testing.TB, path string, cfg IndexWriteConfig, numDocs int, terms []encodingTestTerm) format.HeaderInfo {
	t.Helper()
	cfg.DensityThreshold = 0
	w, err := newStreamingIndexWriter(path, cfg, uint32(numDocs))
	require.NoError(t, err)
	docs := make([]format.DocumentMetadata, numDocs)
	for i := range docs {
		docs[i] = format.DocumentMetadata{ID: uint32(i), MinTimeUnix: int64(i), MaxTimeUnix: int64(i) + 1}
	}
	w.AddDocuments(docs)
	for i, term := range terms {
		switch {
		case term.matchesAll:
			require.NoError(t, w.WriteTermBitmap(term.key, format.Bitmap{MatchesAll: true}))
		case i%2 == 0:
			bm := format.Bitmap{Roaring: roaringOf(term.ids)}
			require.NoError(t, w.WriteTermBitmap(term.key, bm))
		default:
			require.NoError(t, w.WriteTermDocIDs(term.key, term.ids, len(term.ids)))
		}
	}
	require.NoError(t, w.Close())
	return w.Info()
}

// requireIndexTerms checks that r holds exactly terms, via both the term
// iterator (merge path) and random-access GetBitmap (query path).
func requireIndexTerms(t *testing.T, r *IndexReader, terms []encodingTestTerm) {
	t.Helper()
	it, err := r.newTermIterator()
	require.NoError(t, err)
	i := 0
	for it.Next() {
		require.Less(t, i, len(terms))
		require.Equal(t, terms[i].key, it.Term())
		ids, matchesAll := it.DocIDs()
		require.Equal(t, terms[i].matchesAll, matchesAll, "term %d", i)
		if !matchesAll {
			require.Equal(t, terms[i].ids, ids, "term %d", i)
		}
		i++
	}
	require.NoError(t, it.Err())
	require.Equal(t, len(terms), i)

	for _, idx := range []int{len(terms) - 1, 0, len(terms) / 2, 1} {
		bm, err := r.GetBitmap(idx)
		require.NoError(t, err)
		require.Equal(t, terms[idx].matchesAll, bm.MatchesAll)
		if !bm.MatchesAll {
			require.Equal(t, terms[idx].ids, bm.Roaring.ToArray())
		}
	}
}

func TestPostingsEncodings_IndexRoundTrip(t *testing.T) {
	const numDocs, numTerms = 5000, 3000
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(1, 1)), numDocs, numTerms)

	for _, tc := range []struct {
		encoding        format.PostingsEncoding
		wantCompression uint32
	}{
		{format.PostingsEncodingFastDeltaVarIntBlocked, postingsCompressionZstd},
		{format.PostingsEncodingFastEliasFanoBlocked, postingsCompressionNoneXXH3},
	} {
		t.Run(fmt.Sprintf("encoding=%d", tc.encoding), func(t *testing.T) {
			cfg := DefaultFastIndexWriteConfig()
			cfg.Encoding = tc.encoding
			cfg.FastBlockTarget = 16 * 1024 // force many postings blocks
			path := filepath.Join(t.TempDir(), "index.lidx")
			info := writeEncodingTestIndex(t, path, cfg, numDocs, terms)

			encoding, _ := format.ParseFlags(info.Flags)
			require.Equal(t, tc.encoding, encoding)
			require.Equal(t, tc.wantCompression, info.PostingsCompression)
			require.Greater(t, info.PostingsBlockCount, uint32(1))

			// Footer-probing open, as used by tooling.
			r, err := OpenIndexFile(path)
			require.NoError(t, err)
			defer r.Close()
			require.Equal(t, info, r.ReadHeader())
			requireIndexTerms(t, r, terms)

			// meta.json open, as used by the query path.
			f, size := openTestReaderAt(t, path)
			r2, err := OpenIndexAtWithHeader(f, 0, size, info)
			require.NoError(t, err)
			defer r2.Close()
			requireIndexTerms(t, r2, terms)
		})
	}
}

// Existing indexes all record zstd (1), so honoring the field is backward
// compatible. A value that disagrees with how blocks were written must fail
// loudly rather than return wrong postings.
func TestPostingsCompressionIsHonored(t *testing.T) {
	const numDocs = 100
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(2, 2)), numDocs, 50)
	dir := t.TempDir()
	written := map[uint32]format.HeaderInfo{}
	files := map[uint32]string{}
	for _, encoding := range []format.PostingsEncoding{
		format.PostingsEncodingFastDeltaVarIntBlocked,
		format.PostingsEncodingFastEliasFanoBlocked,
	} {
		cfg := DefaultFastIndexWriteConfig()
		cfg.Encoding = encoding
		path := filepath.Join(dir, fmt.Sprintf("index-%d.lidx", encoding))
		info := writeEncodingTestIndex(t, path, cfg, numDocs, terms)
		written[info.PostingsCompression] = info
		files[info.PostingsCompression] = path
	}
	require.Len(t, written, 2, "each encoding writes a different compression")

	for _, compression := range []uint32{0, 3} {
		t.Run(fmt.Sprintf("rejects_%d", compression), func(t *testing.T) {
			bad := written[postingsCompressionZstd]
			bad.PostingsCompression = compression
			f, size := openTestReaderAt(t, files[postingsCompressionZstd])
			_, err := OpenIndexAtWithHeader(f, 0, size, bad)
			require.ErrorContains(t, err, fmt.Sprintf("unsupported postings compression: %d", compression))
		})
	}

	for _, tc := range []struct{ written, claimed uint32 }{
		{postingsCompressionZstd, postingsCompressionNoneXXH3},
		{postingsCompressionNoneXXH3, postingsCompressionZstd},
	} {
		t.Run(fmt.Sprintf("written_%d_read_as_%d", tc.written, tc.claimed), func(t *testing.T) {
			bad := written[tc.written]
			bad.PostingsCompression = tc.claimed
			f, size := openTestReaderAt(t, files[tc.written])
			r, err := OpenIndexAtWithHeader(f, 0, size, bad)
			require.NoError(t, err)
			defer r.Close()
			_, err = r.GetBitmap(1)
			require.Error(t, err)
		})
	}
}

// TestUncompressedBlockFraming pins the on-disk framing of checksummed
// uncompressed postings blocks, independently of the writer:
//
//	u32 storedLen | raw block | u64 xxh3(raw block)
//
// where storedLen covers the raw block and the checksum, and the directory's
// CompressedSize is 4+storedLen. If this fails the format has changed: revert.
func TestUncompressedBlockFraming(t *testing.T) {
	const numDocs = 2000
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(7, 7)), numDocs, 1500)
	cfg := DefaultFastIndexWriteConfig()
	cfg.Encoding = format.PostingsEncodingFastEliasFanoBlocked
	cfg.FastBlockTarget = 8 * 1024
	path := filepath.Join(t.TempDir(), "index.lidx")
	info := writeEncodingTestIndex(t, path, cfg, numDocs, terms)
	require.Greater(t, info.PostingsBlockCount, uint32(1))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	r, err := OpenIndexAt(bytes.NewReader(data), 0, int64(len(data)))
	require.NoError(t, err)
	defer r.Close()

	var total uint64
	for i, entry := range r.metadata.postingsDir {
		block := data[entry.BlockOffset : entry.BlockOffset+uint64(entry.CompressedSize)]
		storedLen := binary.LittleEndian.Uint32(block[:4])
		require.Equal(t, entry.CompressedSize, 4+storedLen, "block %d", i)
		raw := block[4 : len(block)-postingsBlockChecksumSize]
		require.Equal(t, xxh3.Hash(raw), binary.LittleEndian.Uint64(block[len(block)-postingsBlockChecksumSize:]), "block %d", i)
		require.Equal(t, entry.NumTerms, binary.LittleEndian.Uint32(raw[:4]), "block %d", i)
		total += uint64(entry.CompressedSize)
	}
	require.Equal(t, info.PostingsDataSize, total)
}

// A single flipped bit anywhere in an uncompressed block, including in the
// Elias-Fano low bits where it would otherwise decode to a valid but wrong
// docID, must fail the read.
func TestUncompressedBlockChecksumDetectsCorruption(t *testing.T) {
	const numDocs = 2000
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(8, 8)), numDocs, 300)
	cfg := DefaultFastIndexWriteConfig()
	cfg.Encoding = format.PostingsEncodingFastEliasFanoBlocked
	path := filepath.Join(t.TempDir(), "index.lidx")
	writeEncodingTestIndex(t, path, cfg, numDocs, terms)
	clean, err := os.ReadFile(path)
	require.NoError(t, err)

	r, err := OpenIndexAt(bytes.NewReader(clean), 0, int64(len(clean)))
	require.NoError(t, err)
	entry := r.metadata.postingsDir[0]
	r.Close()

	start := int(entry.BlockOffset) + 4
	end := int(entry.BlockOffset + uint64(entry.CompressedSize))
	for _, off := range []int{start, start + 4, (start + end) / 2, end - postingsBlockChecksumSize - 1, end - 1} {
		t.Run(fmt.Sprintf("offset=%d", off), func(t *testing.T) {
			corrupt := slices.Clone(clean)
			corrupt[off] ^= 0x01
			r, err := OpenIndexAt(bytes.NewReader(corrupt), 0, int64(len(corrupt)))
			require.NoError(t, err, "postings are not read at open")
			defer r.Close()
			_, err = r.GetBitmap(1)
			require.ErrorContains(t, err, "checksum mismatch")
		})
	}
}

func TestUnknownPostingsEncodingIsRejected(t *testing.T) {
	const numDocs = 100
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(3, 3)), numDocs, 50)
	path := filepath.Join(t.TempDir(), "index.lidx")
	info := writeEncodingTestIndex(t, path, DefaultFastIndexWriteConfig(), numDocs, terms)
	f, size := openTestReaderAt(t, path)

	bad := info
	bad.Flags = composeIndexFlags(format.PostingsEncodingFastEliasFanoBlocked+1, 1)
	_, err := OpenIndexAtWithHeader(f, 0, size, bad)
	require.ErrorContains(t, err, "unsupported postings encoding")
}

// Compaction reads sources of either encoding and writes one. Every mix must
// produce the same postings as merging delta-varint sources only.
func TestMergeMixedPostingsEncodings(t *testing.T) {
	const numDocs, numTerms = 800, 600
	dv := format.PostingsEncodingFastDeltaVarIntBlocked
	ef := format.PostingsEncodingFastEliasFanoBlocked
	dir := t.TempDir()

	// Two sources with overlapping terms and documents.
	sources := [][]encodingTestTerm{
		buildEncodingTestTerms(rand.New(rand.NewPCG(4, 4)), numDocs, numTerms),
		buildEncodingTestTerms(rand.New(rand.NewPCG(5, 5)), numDocs, numTerms/2),
	}
	writeSource := func(i int, encoding format.PostingsEncoding) string {
		cfg := DefaultFastIndexWriteConfig()
		cfg.Encoding = encoding
		cfg.FastBlockTarget = 4 * 1024
		path := filepath.Join(dir, fmt.Sprintf("src%d-%d.lidx", i, encoding))
		writeEncodingTestIndex(t, path, cfg, numDocs, sources[i])
		return path
	}
	merge := func(inputs []string, out format.PostingsEncoding) []byte {
		cfg := DefaultFastIndexWriteConfig()
		cfg.Encoding = out
		var buf bytes.Buffer
		info, err := mergeFilesTo(context.Background(), t, inputs, &buf, cfg)
		require.NoError(t, err)
		encoding, _ := format.ParseFlags(info.Flags)
		require.Equal(t, out, encoding)
		return buf.Bytes()
	}
	decodeAll := func(data []byte) map[[8]byte][]uint32 {
		r, err := OpenIndexAt(bytes.NewReader(data), 0, int64(len(data)))
		require.NoError(t, err)
		defer r.Close()
		it, err := r.newTermIterator()
		require.NoError(t, err)
		got := map[[8]byte][]uint32{}
		for it.Next() {
			ids, matchesAll := it.DocIDs()
			if matchesAll {
				ids = nil
			}
			got[it.Term()] = append([]uint32{}, ids...)
		}
		require.NoError(t, it.Err())
		return got
	}

	want := decodeAll(merge([]string{writeSource(0, dv), writeSource(1, dv)}, dv))
	require.NotEmpty(t, want)
	for _, tc := range []struct {
		name   string
		src0   format.PostingsEncoding
		src1   format.PostingsEncoding
		outEnc format.PostingsEncoding
	}{
		{name: "dv+ef->ef", src0: dv, src1: ef, outEnc: ef},
		{name: "ef+ef->ef", src0: ef, src1: ef, outEnc: ef},
		{name: "ef+dv->dv (rollback)", src0: ef, src1: dv, outEnc: dv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAll(merge([]string{writeSource(0, tc.src0), writeSource(1, tc.src1)}, tc.outEnc))
			require.Equal(t, want, got)
		})
	}
}

// BenchmarkPostingsDecode measures full postings decode of one index per
// encoding: every block read, decompressed if needed, and every term decoded.
// This is the per-block cost the query path and compaction pay.
func BenchmarkPostingsDecode(b *testing.B) {
	const numDocs, numTerms = 50_000, 50_000
	terms := buildEncodingTestTerms(rand.New(rand.NewPCG(6, 6)), numDocs, numTerms)
	for _, encoding := range []format.PostingsEncoding{
		format.PostingsEncodingFastDeltaVarIntBlocked,
		format.PostingsEncodingFastEliasFanoBlocked,
	} {
		cfg := DefaultFastIndexWriteConfig()
		cfg.Encoding = encoding
		path := filepath.Join(b.TempDir(), "index.lidx")
		info := writeEncodingTestIndex(b, path, cfg, numDocs, terms)

		b.Run(fmt.Sprintf("encoding=%d", encoding), func(b *testing.B) {
			data, err := os.ReadFile(path)
			require.NoError(b, err)
			for b.Loop() {
				// A fresh reader per iteration so no block is served from cache.
				r, err := OpenIndexAt(bytes.NewReader(data), 0, int64(len(data)))
				if err != nil {
					b.Fatal(err)
				}
				var buf []uint32
				for i := range int(info.TermCount) {
					if buf, _, err = r.postings.GetDocIDs(i, buf); err != nil {
						b.Fatal(err)
					}
				}
				r.Close()
			}
			b.ReportMetric(float64(info.PostingsDataSize), "postings-bytes")
			b.ReportMetric(float64(info.PostingsBlockCount), "blocks")
		})
	}
}
