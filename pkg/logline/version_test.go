package logline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

type testIndexSource struct {
	docs  []format.DocumentMetadata
	terms map[[8]byte][]uint32
}

type docTimeKey struct {
	min int64
	max int64
}

func TestWriter_MinimalRoundTrip(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200}},
				terms: map[[8]byte][]uint32{
					term8("MIN000"): {0},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			require.Equal(t, uint32(1), header.DocumentCount)
			require.Equal(t, uint64(1), header.TermCount)
			require.Equal(t, expectedDocIDs(source, []string{"MIN000"}), openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"MIN000"}))
		})
	}
}

func TestWriter_BasicRoundTrip(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 150},
					{ID: 1, MinTimeUnix: 200, MaxTimeUnix: 250},
					{ID: 2, MinTimeUnix: 300, MaxTimeUnix: 350},
				},
				terms: map[[8]byte][]uint32{
					term8("ALPHA0"): {0, 2},
					term8("BETA00"): {1},
					term8("GAMMA0"): {1, 2},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			size := fileSize(t, path)

			require.Equal(t, uint32(3), header.DocumentCount)
			require.Equal(t, uint64(3), header.TermCount)
			require.True(t, header.PostingsDataSize > 0)
			require.True(t, header.TermDataSize > 0)
			require.True(t, header.DocMetadataSize > 0)

			require.Equal(t, expectedDocIDs(source, []string{"ALPHA0"}), openAndQueryTerms(t, version, path, size, header, []string{"ALPHA0"}))
			require.Equal(t, expectedDocIDs(source, []string{"GAMMA0"}), openAndQueryTerms(t, version, path, size, header, []string{"GAMMA0"}))
			require.Equal(t, expectedDocIDs(source, []string{"ALPHA0", "GAMMA0"}), openAndQueryTerms(t, version, path, size, header, []string{"ALPHA0", "GAMMA0"}))
		})
	}
}

func TestWriter_DocMetadataPreserved(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 1700000000000, MaxTimeUnix: 1700000000123},
					{ID: 1, MinTimeUnix: 1700000001000, MaxTimeUnix: 1700000001456},
				},
				terms: map[[8]byte][]uint32{
					term8("DOCMET"): {0, 1},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			reader, _, file := openReader(t, version, path, fileSize(t, path), header)
			defer func() {
				require.NoError(t, reader.Close())
				require.NoError(t, file.Close())
			}()

			docs := reader.Documents()
			require.Len(t, docs, len(source.docs))
			for i, doc := range docs {
				require.Equal(t, source.docs[i].MinTimeUnix, doc.MinTimeUnix)
				require.Equal(t, source.docs[i].MaxTimeUnix, doc.MaxTimeUnix)
			}

			require.Equal(t, expectedDocIDs(source, []string{"DOCMET"}), queryTerms(t, reader, []string{"DOCMET"}))
		})
	}
}

func TestWriter_EmptyIndex(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			path, header := writeIndex(t, version, nil, nil)

			require.Equal(t, uint32(0), header.DocumentCount)
			require.Equal(t, uint64(0), header.TermCount)

			reader, _, file := openReader(t, version, path, fileSize(t, path), header)
			defer func() {
				require.NoError(t, reader.Close())
				require.NoError(t, file.Close())
			}()
			require.Empty(t, reader.Documents())
		})
	}
}

func TestWriter_TermOrderingEnforced(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ordering.lidx")
			writer, err := NewWriter(version, path, []format.DocumentMetadata{{ID: 0}, {ID: 1}}, nil)
			require.NoError(t, err)

			bm := roaring.New()
			bm.Add(0)

			require.NoError(t, writer.WriteTermBitmap(term8("ZZZZZZ"), format.Bitmap{Roaring: bm}))

			err = writer.WriteTermBitmap(term8("AAAAAA"), format.Bitmap{Roaring: bm})
			require.Error(t, err)
			require.Contains(t, err.Error(), "out-of-order")

			err = writer.WriteTermBitmap(term8("ZZZZZZ"), format.Bitmap{Roaring: bm})
			require.Error(t, err)
			require.Contains(t, err.Error(), "out-of-order")

			err = writer.Close()
			require.Error(t, err)
			_, statErr := os.Stat(path)
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestWriter_FailedWriteNoPartialFile(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "partial.lidx")
			writer, err := NewWriter(version, path, []format.DocumentMetadata{{ID: 0}, {ID: 1}}, nil)
			require.NoError(t, err)

			bm := roaring.New()
			bm.Add(0)

			require.NoError(t, writer.WriteTermBitmap(term8("BBBBBB"), format.Bitmap{Roaring: bm}))
			require.Error(t, writer.WriteTermBitmap(term8("AAAAAA"), format.Bitmap{Roaring: bm}))
			require.Error(t, writer.Close())

			_, statErr := os.Stat(path)
			if statErr == nil {
				file, err := os.Open(path)
				require.NoError(t, err)
				defer func() {
					require.NoError(t, file.Close())
				}()

				_, _, _, err = OpenReaderAt(file, 0, fileSize(t, path))
				require.Error(t, err)
				return
			}

			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestWriter_LargeBitmapRoundTrip(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			const docCount = 10000

			docs := make([]format.DocumentMetadata, docCount)
			docIDs := make([]uint32, docCount)
			for i := range docCount {
				docs[i] = format.DocumentMetadata{
					ID:          uint32(i),
					MinTimeUnix: int64(i) * 100,
					MaxTimeUnix: int64(i)*100 + 50,
				}
				docIDs[i] = uint32(i)
			}

			source := testIndexSource{
				docs: docs,
				terms: map[[8]byte][]uint32{
					term8("FANOUT"): docIDs,
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			require.Equal(t, uint32(docCount), header.DocumentCount)
			require.Equal(t, expectedDocIDs(source, []string{"FANOUT"}), openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"FANOUT"}))
		})
	}
}

func TestWriter_LargeTermSetRoundTrip(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			const (
				docCount  = 128
				termCount = 10000
			)

			docs := make([]format.DocumentMetadata, docCount)
			for i := range docCount {
				docs[i] = format.DocumentMetadata{
					ID:          uint32(i),
					MinTimeUnix: int64(i) * 1000,
					MaxTimeUnix: int64(i)*1000 + 500,
				}
			}

			terms := make(map[[8]byte][]uint32, termCount)
			for i := range termCount {
				terms[term8(fmt.Sprintf("T%05d", i))] = []uint32{uint32(i % docCount)}
			}

			source := testIndexSource{docs: docs, terms: terms}
			path, header := writeIndex(t, version, source.docs, source.terms)
			size := fileSize(t, path)

			require.Equal(t, uint64(termCount), header.TermCount)

			reader, _, file := openReader(t, version, path, size, header)
			defer func() {
				require.NoError(t, reader.Close())
				require.NoError(t, file.Close())
			}()

			for i := range termCount {
				term := fmt.Sprintf("T%05d", i)
				require.Equal(t, expectedDocIDs(source, []string{term}), queryTerms(t, reader, []string{term}))
			}
		})
	}
}

func TestReader_OpenAndCachedReopenMatch(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					{ID: 2, MinTimeUnix: 500, MaxTimeUnix: 600},
				},
				terms: map[[8]byte][]uint32{
					term8("AAAAAA"): {0, 1},
					term8("BBBBBB"): {1, 2},
					term8("CCCCCC"): {1},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			size := fileSize(t, path)

			reader, cached, file := openReader(t, version, path, size, header)
			openHeader := reader.ReadHeader()
			openDocIDs := queryTerms(t, reader, []string{"AAAAAA", "CCCCCC"})
			require.NoError(t, reader.Close())
			require.NoError(t, file.Close())

			cachedDocIDs, cachedHeader := openAndQueryTermsCached(t, version, path, size, cached, []string{"AAAAAA", "CCCCCC"})

			require.Equal(t, openHeader, cachedHeader)
			require.Equal(t, openDocIDs, cachedDocIDs)
		})
	}
}

func TestReader_CloseIsIdempotent(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2}},
				terms: map[[8]byte][]uint32{term8("CLOSE0"): {0}},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			reader, _, file := openReader(t, version, path, fileSize(t, path), header)

			require.NoError(t, reader.Close())
			require.NoError(t, reader.Close())
			require.NoError(t, file.Close())
		})
	}
}

func TestReader_FindTerm_Miss(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
				},
				terms: map[[8]byte][]uint32{
					term8("AAAAAA"): {0, 1},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			reader, _, file := openReader(t, version, path, fileSize(t, path), header)
			defer func() {
				require.NoError(t, reader.Close())
				require.NoError(t, file.Close())
			}()

			idx, err := reader.FindTerm("MISSNG")
			require.NoError(t, err)
			require.Equal(t, -1, idx)

			require.Nil(t, queryTerms(t, reader, []string{"AAAAAA", "MISSNG"}))
		})
	}
}

func TestReader_AND_DisjointTerms(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
				},
				terms: map[[8]byte][]uint32{
					term8("AAAAAA"): {0},
					term8("BBBBBB"): {1},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			require.Nil(t, openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"AAAAAA", "BBBBBB"}))
		})
	}
}

func TestReader_AND_MultipleTerms(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					{ID: 2, MinTimeUnix: 500, MaxTimeUnix: 600},
				},
				terms: map[[8]byte][]uint32{
					term8("AAAAAA"): {0, 1},
					term8("BBBBBB"): {1},
					term8("CCCCCC"): {1, 2},
				},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			size := fileSize(t, path)

			require.Equal(t,
				expectedDocIDs(source, []string{"AAAAAA", "BBBBBB", "CCCCCC"}),
				openAndQueryTerms(t, version, path, size, header, []string{"AAAAAA", "BBBBBB", "CCCCCC"}),
			)
		})
	}
}

func TestReader_QueryNoTerms(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2}},
				terms: map[[8]byte][]uint32{term8("AAAAAA"): {0}},
			}

			path, header := writeIndex(t, version, source.docs, source.terms)
			require.Nil(t, openAndQueryTerms(t, version, path, fileSize(t, path), header, nil))
		})
	}
}

func TestMerger_TwoSources_CanReopen(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 200, MaxTimeUnix: 300},
						{ID: 2, MinTimeUnix: 300, MaxTimeUnix: 400},
					},
					terms: map[[8]byte][]uint32{
						term8("TERMAA"): {0, 1},
						term8("TERMBB"): {2},
					},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 500, MaxTimeUnix: 600},
						{ID: 1, MinTimeUnix: 600, MaxTimeUnix: 700},
						{ID: 2, MinTimeUnix: 700, MaxTimeUnix: 800},
						{ID: 3, MinTimeUnix: 800, MaxTimeUnix: 900},
						{ID: 4, MinTimeUnix: 900, MaxTimeUnix: 1000},
					},
					terms: map[[8]byte][]uint32{
						term8("TERMAA"): {0, 2},
						term8("TERMCC"): {1, 3, 4},
					},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)
			size := fileSize(t, path)

			require.Equal(t, uint32(8), header.DocumentCount)
			require.Equal(t, expectedDocIDs(expected, []string{"TERMAA"}), openAndQueryTerms(t, version, path, size, header, []string{"TERMAA"}))
			require.Equal(t, expectedDocIDs(expected, []string{"TERMCC"}), openAndQueryTerms(t, version, path, size, header, []string{"TERMCC"}))
		})
	}
}

func TestMerger_DocIDsRemappedSequentially(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					},
					terms: map[[8]byte][]uint32{term8("ALLDOC"): {0, 1}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 500, MaxTimeUnix: 600},
						{ID: 1, MinTimeUnix: 700, MaxTimeUnix: 800},
						{ID: 2, MinTimeUnix: 900, MaxTimeUnix: 1000},
					},
					terms: map[[8]byte][]uint32{term8("ALLDOC"): {0, 1, 2}},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)
			docIDs := openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"ALLDOC"})

			require.Equal(t, expectedDocIDs(expected, []string{"ALLDOC"}), docIDs)
			for i, id := range docIDs {
				require.Equal(t, uint32(i), id)
			}
		})
	}
}

func TestMerger_QueryResultsUseRemappedDocIDs(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					},
					terms: map[[8]byte][]uint32{term8("SHARED"): {0, 1}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 500, MaxTimeUnix: 600},
					},
					terms: map[[8]byte][]uint32{term8("SHARED"): {0}},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)
			docIDs := openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"SHARED"})

			require.Equal(t, expectedDocIDs(expected, []string{"SHARED"}), docIDs)
			require.Equal(t, []uint32{0, 1, 2}, docIDs)
		})
	}
}

func TestMerger_DisjointTermsUnion(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200}},
					terms: map[[8]byte][]uint32{term8("AAAAAA"): {0}},
				},
				{
					docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 300, MaxTimeUnix: 400}},
					terms: map[[8]byte][]uint32{term8("BBBBBB"): {0}},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)
			size := fileSize(t, path)

			require.Equal(t, uint64(2), header.TermCount)
			require.Equal(t, expectedDocIDs(expected, []string{"AAAAAA"}), openAndQueryTerms(t, version, path, size, header, []string{"AAAAAA"}))
			require.Equal(t, expectedDocIDs(expected, []string{"BBBBBB"}), openAndQueryTerms(t, version, path, size, header, []string{"BBBBBB"}))
		})
	}
}

func TestMerger_DeduplicatesDocsByTimeBounds(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					},
					terms: map[[8]byte][]uint32{term8("FOOBAR"): {0, 1}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 500, MaxTimeUnix: 600},
					},
					terms: map[[8]byte][]uint32{
						term8("FOOBAR"): {1},
						term8("BAZQUX"): {0},
					},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)
			size := fileSize(t, path)

			require.Equal(t, uint32(3), header.DocumentCount)
			fooIDs := openAndQueryTerms(t, version, path, size, header, []string{"FOOBAR"})
			bazIDs := openAndQueryTerms(t, version, path, size, header, []string{"BAZQUX"})

			require.Equal(t, expectedDocIDs(expected, []string{"FOOBAR"}), fooIDs)
			require.Equal(t, expectedDocIDs(expected, []string{"BAZQUX"}), bazIDs)
			require.Equal(t, bazIDs[0], fooIDs[0])
		})
	}
}

func TestMerger_EmptyInputs(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			cases := []struct {
				name    string
				sources []testIndexSource
				terms   []string
			}{
				{
					name: "empty_plus_empty",
					sources: []testIndexSource{
						{},
						{},
					},
				},
				{
					name: "empty_plus_non_empty",
					sources: []testIndexSource{
						{},
						{
							docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 10, MaxTimeUnix: 20}},
							terms: map[[8]byte][]uint32{term8("NONEMP"): {0}},
						},
					},
					terms: []string{"NONEMP"},
				},
			}

			for _, tc := range cases {

				t.Run(tc.name, func(t *testing.T) {
					expected := expectedMergedIndex(tc.sources)
					path, header := writeAndMerge(t, version, tc.sources)
					size := fileSize(t, path)

					require.Equal(t, uint32(len(expected.docs)), header.DocumentCount)
					require.Equal(t, uint64(len(expected.terms)), header.TermCount)
					require.Equal(t, expectedDocIDs(expected, tc.terms), openAndQueryTerms(t, version, path, size, header, tc.terms))
				})
			}
		})
	}
}

func TestMerger_TooFewReaders(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			merger, err := NewMerger(version, nil)
			require.NoError(t, err)

			var out discardWriter
			_, err = merger.Merge(context.Background(), nil, nil, &out)
			require.ErrorContains(t, err, "at least 2 readers")

			sourcePath, _ := writeIndex(t, version, []format.DocumentMetadata{{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2}}, map[[8]byte][]uint32{term8("AAAAAA"): {0}})
			reader, size := openReaderAt(t, sourcePath)

			_, err = merger.Merge(context.Background(), []io.ReaderAt{reader}, []int64{size}, &out)
			require.ErrorContains(t, err, "at least 2 readers")
		})
	}
}

func TestMerger_MismatchedReaderSizeCounts(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			merger, err := NewMerger(version, nil)
			require.NoError(t, err)

			pathA, _ := writeIndex(t, version, []format.DocumentMetadata{{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2}}, map[[8]byte][]uint32{term8("AAAAAA"): {0}})
			pathB, _ := writeIndex(t, version, []format.DocumentMetadata{{ID: 0, MinTimeUnix: 3, MaxTimeUnix: 4}}, map[[8]byte][]uint32{term8("BBBBBB"): {0}})
			readerA, sizeA := openReaderAt(t, pathA)
			readerB, _ := openReaderAt(t, pathB)

			var out discardWriter
			_, err = merger.Merge(context.Background(), []io.ReaderAt{readerA, readerB}, []int64{sizeA}, &out)
			require.ErrorContains(t, err, "mismatch")
		})
	}
}

// discardWriter drops every byte written to it. Used by error-path merger
// tests that fail before any output bytes are produced.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestMerger_ThreeSources(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200}},
					terms: map[[8]byte][]uint32{term8("AAAAAA"): {0}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 300, MaxTimeUnix: 400},
						{ID: 1, MinTimeUnix: 400, MaxTimeUnix: 500},
					},
					terms: map[[8]byte][]uint32{term8("AAAAAA"): {0, 1}},
				},
				{
					docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 600, MaxTimeUnix: 700}},
					terms: map[[8]byte][]uint32{term8("AAAAAA"): {0}},
				},
			}

			expected := expectedMergedIndex(sources)
			path, header := writeAndMerge(t, version, sources)

			require.Equal(t, uint32(4), header.DocumentCount)
			require.Equal(t, expectedDocIDs(expected, []string{"AAAAAA"}), openAndQueryTerms(t, version, path, fileSize(t, path), header, []string{"AAAAAA"}))
		})
	}
}

func TestMerger_MergedCanBeMergedAgain(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 200, MaxTimeUnix: 300},
					},
					terms: map[[8]byte][]uint32{term8("TERMAA"): {0, 1}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 400, MaxTimeUnix: 500},
						{ID: 1, MinTimeUnix: 500, MaxTimeUnix: 600},
						{ID: 2, MinTimeUnix: 600, MaxTimeUnix: 700},
					},
					terms: map[[8]byte][]uint32{term8("TERMAA"): {0, 1, 2}},
				},
			}
			sourceC := testIndexSource{
				docs:  []format.DocumentMetadata{{ID: 0, MinTimeUnix: 800, MaxTimeUnix: 900}},
				terms: map[[8]byte][]uint32{term8("TERMAA"): {0}},
			}

			mergedPath, _ := writeAndMerge(t, version, sources)
			pathC, _ := writeIndex(t, version, sourceC.docs, sourceC.terms)
			remergedPath, header := mergePaths(context.Background(), t, version, []string{mergedPath, pathC})

			expected := expectedMergedIndex(append(sources, sourceC))

			require.Equal(t, uint32(6), header.DocumentCount)
			require.Equal(t, expectedDocIDs(expected, []string{"TERMAA"}), openAndQueryTerms(t, version, remergedPath, fileSize(t, remergedPath), header, []string{"TERMAA"}))
		})
	}
}

func TestMerger_CancelledContext(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			pathA, _ := writeIndex(t, version, []format.DocumentMetadata{{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200}}, map[[8]byte][]uint32{term8("AAAAAA"): {0}})
			pathB, _ := writeIndex(t, version, []format.DocumentMetadata{{ID: 0, MinTimeUnix: 300, MaxTimeUnix: 400}}, map[[8]byte][]uint32{term8("AAAAAA"): {0}})
			readerA, sizeA := openReaderAt(t, pathA)
			readerB, sizeB := openReaderAt(t, pathB)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			merger, err := NewMerger(version, nil)
			require.NoError(t, err)
			var out discardWriter
			_, err = merger.Merge(ctx, []io.ReaderAt{readerA, readerB}, []int64{sizeA, sizeB}, &out)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestOpenReaderAt_Valid(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			source := testIndexSource{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
				},
				terms: map[[8]byte][]uint32{
					term8("TERMAA"): {0, 1},
					term8("TERMBB"): {1},
				},
			}

			path, _ := writeIndex(t, version, source.docs, source.terms)
			file, err := os.Open(path)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, file.Close())
			}()

			reader, _, _, err := OpenReaderAt(file, 0, fileSize(t, path))
			require.NoError(t, err)
			header := reader.ReadHeader()
			reader.Close()
			require.Equal(t, uint32(2), header.DocumentCount)
			require.Equal(t, uint64(2), header.TermCount)
			require.True(t, header.PostingsDataSize > 0)
			require.True(t, header.TermDataSize > 0)
			require.True(t, header.DocMetadataSize > 0)
		})
	}
}

func TestOpenReaderAt_NotAnIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.lidx")
	garbage := make([]byte, 1000)
	copy(garbage, "not an index")
	require.NoError(t, os.WriteFile(path, garbage, 0o644))

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, file.Close())
	}()

	_, _, _, err = OpenReaderAt(file, 0, fileSize(t, path))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported index format")
}

func TestOpenReaderAt_TooSmall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tiny.lidx")
	require.NoError(t, os.WriteFile(path, []byte("tiny"), 0o644))

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, file.Close())
	}()

	_, _, _, err = OpenReaderAt(file, 0, fileSize(t, path))
	require.Error(t, err)
}

func TestNewWriter_UnsupportedVersion(t *testing.T) {
	_, err := NewWriter("v-does-not-exist", filepath.Join(t.TempDir(), "index.lidx"), nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported index version")
}

func TestOpenReader_UnsupportedVersion(t *testing.T) {
	reader := bytes.NewReader(nil)
	_, _, err := OpenReader("v-does-not-exist", reader, 0, 0, format.HeaderInfo{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported index version")
}

func TestOpenReaderCached_UnsupportedVersion(t *testing.T) {
	reader := bytes.NewReader(nil)
	_, err := OpenReaderCached("v-does-not-exist", reader, 0, 0, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported index version")
}

func TestOpenReaderCached_InvalidCachedState(t *testing.T) {
	reader := bytes.NewReader(nil)
	_, err := OpenReaderCached(CurrentVersion, reader, 0, 0, "not cached state")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid cached state type")
}

func TestNewMerger_UnsupportedVersion(t *testing.T) {
	_, err := NewMerger("v-does-not-exist", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported index version")
}

func TestValidateVersion_Known(t *testing.T) {
	for _, v := range AllVersions() {
		require.NoError(t, ValidateVersion(v))
	}
}

func TestValidateVersion_Unknown(t *testing.T) {
	require.Error(t, ValidateVersion("v99"))
}

func TestMerge_Comprehensive(t *testing.T) {
	for _, version := range AllVersions() {

		t.Run(version, func(t *testing.T) {
			sources := buildComprehensiveSources()
			expected := expectedMergedIndex(sources)

			path, header := writeAndMerge(t, version, sources)
			size := fileSize(t, path)

			require.Equal(t, uint32(len(expected.docs)), header.DocumentCount)
			require.Equal(t, uint64(len(expected.terms)), header.TermCount)
			require.True(t, header.PostingsDataSize > 0)
			require.True(t, header.TermDataSize > 0)
			require.True(t, header.DocMetadataSize > 0)

			for _, key := range sortedTerms(expected.terms) {
				term := termString(key)
				require.Equal(t, expectedDocIDs(expected, []string{term}), openAndQueryTerms(t, version, path, size, header, []string{term}))
			}

			andTerms := []string{"S00000", "S00001"}
			require.Equal(t, expectedDocIDs(expected, andTerms), openAndQueryTerms(t, version, path, size, header, andTerms))
			require.Nil(t, openAndQueryTerms(t, version, path, size, header, []string{"ZZZZZZ"}))

			reader, cached, file := openReader(t, version, path, size, header)
			openDocIDs := queryTerms(t, reader, andTerms)
			require.NoError(t, reader.Close())
			require.NoError(t, file.Close())

			cachedDocIDs, cachedHeader := openAndQueryTermsCached(t, version, path, size, cached, andTerms)
			require.Equal(t, header, cachedHeader)
			require.Equal(t, openDocIDs, cachedDocIDs)

			sourcePath, _ := writeIndex(t, version, sources[0].docs, sources[0].terms)
			remergedPath, remergedHeader := mergePaths(context.Background(), t, version, []string{path, sourcePath})
			remergedExpected := expectedMergedIndex([]testIndexSource{expected, sources[0]})

			require.Equal(t, uint32(len(remergedExpected.docs)), remergedHeader.DocumentCount)
			require.Equal(t, uint64(len(remergedExpected.terms)), remergedHeader.TermCount)
			require.Equal(t,
				expectedDocIDs(remergedExpected, andTerms),
				openAndQueryTerms(t, version, remergedPath, fileSize(t, remergedPath), remergedHeader, andTerms),
			)
		})
	}
}

func FuzzWriteAndQuery(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint8(1), uint8(1))
	f.Add(uint8(8), uint8(1), uint8(2))
	f.Add(uint8(4), uint8(2), uint8(3))
	f.Add(uint8(4), uint8(2), uint8(4))
	f.Add(uint8(5), uint8(3), uint8(5))
	f.Add(uint8(8), uint8(4), uint8(6))

	f.Fuzz(func(t *testing.T, docSeed, termSeed, modeSeed uint8) {
		source, queries := buildFuzzWriteCase(docSeed, termSeed, modeSeed)

		for _, version := range AllVersions() {

			t.Run(version, func(t *testing.T) {
				path, header := writeIndex(t, version, source.docs, source.terms)
				size := fileSize(t, path)

				for _, terms := range queries {
					require.Equal(t,
						expectedDocIDs(source, terms),
						openAndQueryTerms(t, version, path, size, header, terms),
					)
				}
			})
		}
	})
}

func FuzzMergeAndQuery(f *testing.F) {
	f.Add(uint8(2), uint8(0), uint8(0))
	f.Add(uint8(2), uint8(1), uint8(1))
	f.Add(uint8(2), uint8(2), uint8(2))
	f.Add(uint8(2), uint8(3), uint8(3))
	f.Add(uint8(2), uint8(2), uint8(4))
	f.Add(uint8(3), uint8(4), uint8(5))
	f.Add(uint8(3), uint8(4), uint8(6))

	f.Fuzz(func(t *testing.T, sourceSeed, docSeed, modeSeed uint8) {
		sources, queries, remerge := buildFuzzMergeCase(sourceSeed, docSeed, modeSeed)
		expected := expectedMergedIndex(sources)

		for _, version := range AllVersions() {

			t.Run(version, func(t *testing.T) {
				path, header := writeAndMerge(t, version, sources)
				size := fileSize(t, path)

				for _, terms := range queries {
					require.Equal(t, expectedDocIDs(expected, terms), openAndQueryTerms(t, version, path, size, header, terms))

					reader, cached, file := openReader(t, version, path, size, header)
					openDocIDs := queryTerms(t, reader, terms)
					require.NoError(t, reader.Close())
					require.NoError(t, file.Close())

					cachedDocIDs, cachedHeader := openAndQueryTermsCached(t, version, path, size, cached, terms)
					require.Equal(t, header, cachedHeader)
					require.Equal(t, openDocIDs, cachedDocIDs)
				}

				if remerge && len(queries) > 0 && len(sources) > 0 {
					sourcePath, _ := writeIndex(t, version, sources[0].docs, sources[0].terms)
					remergedPath, remergedHeader := mergePaths(context.Background(), t, version, []string{path, sourcePath})
					remergedExpected := expectedMergedIndex([]testIndexSource{expected, sources[0]})
					require.Equal(t,
						expectedDocIDs(remergedExpected, queries[0]),
						openAndQueryTerms(t, version, remergedPath, fileSize(t, remergedPath), remergedHeader, queries[0]),
					)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func writeIndex(t *testing.T, version string, docs []format.DocumentMetadata, terms map[[8]byte][]uint32) (string, format.HeaderInfo) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "index.lidx")
	writeIndexAtPath(t, version, path, docs, terms)
	return path, readHeaderFromPath(t, path)
}

func writeIndexAtPath(t *testing.T, version, path string, docs []format.DocumentMetadata, terms map[[8]byte][]uint32) {
	t.Helper()

	// Disable density filter so general-correctness tests get exact intersection semantics.
	// Density-filter behaviour is covered by dedicated tests in pkg/logline/internal/v3/.
	writer, err := NewWriter(version, path, docs, &format.WriterConfig{DensityThreshold: -1})
	require.NoError(t, err)

	for _, term := range sortedTerms(terms) {
		require.NoError(t, writer.WriteTermBitmap(term, format.Bitmap{Roaring: bitmapForDocIDs(terms[term])}))
	}

	require.NoError(t, writer.Close())
}

func queryTerms(t *testing.T, reader Reader, terms []string) []uint32 {
	t.Helper()
	if len(terms) == 0 {
		return nil
	}

	result := format.Bitmap{MatchesAll: true}
	for _, term := range terms {
		idx, err := reader.FindTerm(term)
		require.NoError(t, err)
		if idx < 0 {
			return nil
		}
		res, err := reader.GetBitmap(idx)
		require.NoError(t, err)
		result = result.And(res)
		if result.IsEmpty() {
			return nil
		}
	}
	if result.MatchesAll {
		return nil // all terms were sentinels; caller should handle
	}
	return result.Roaring.ToArray()
}

func openAndQueryTerms(t *testing.T, version, path string, size int64, headerInfo format.HeaderInfo, terms []string) []uint32 {
	t.Helper()

	reader, _, file := openReader(t, version, path, size, headerInfo)
	defer func() {
		require.NoError(t, reader.Close())
		require.NoError(t, file.Close())
	}()

	return queryTerms(t, reader, terms)
}

func openAndQueryTermsCached(t *testing.T, version, path string, size int64, cached any, terms []string) ([]uint32, format.HeaderInfo) {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, file.Close())
	}()

	reader, err := OpenReaderCached(version, file, 0, size, cached)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, reader.Close())
	}()

	return queryTerms(t, reader, terms), reader.ReadHeader()
}

func openReader(t *testing.T, version, path string, size int64, headerInfo format.HeaderInfo) (Reader, any, *os.File) {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)

	reader, cached, err := OpenReader(version, file, 0, size, headerInfo)
	require.NoError(t, err)
	return reader, cached, file
}

func writeAndMerge(t *testing.T, version string, sources []testIndexSource) (string, format.HeaderInfo) {
	t.Helper()

	paths := make([]string, len(sources))
	for i, source := range sources {
		path, _ := writeIndex(t, version, source.docs, source.terms)
		paths[i] = path
	}

	return mergePaths(context.Background(), t, version, paths)
}

func mergePaths(ctx context.Context, t *testing.T, version string, paths []string) (string, format.HeaderInfo) {
	t.Helper()

	readers := make([]io.ReaderAt, len(paths))
	sizes := make([]int64, len(paths))
	for i, path := range paths {
		file, size := openReaderAt(t, path)
		readers[i] = file
		sizes[i] = size
	}

	// Disable density filter so general-correctness tests get exact intersection semantics.
	merger, err := NewMerger(version, &format.WriterConfig{DensityThreshold: -1})
	require.NoError(t, err)

	outputPath := filepath.Join(t.TempDir(), "merged.lidx")
	f, err := os.Create(outputPath)
	require.NoError(t, err)
	_, err = merger.Merge(ctx, readers, sizes, f)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	return outputPath, readHeaderFromPath(t, outputPath)
}

func openReaderAt(t *testing.T, path string) (*os.File, int64) {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, file.Close())
	})

	info, err := file.Stat()
	require.NoError(t, err)
	return file, info.Size()
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

func readHeaderFromPath(t *testing.T, path string) format.HeaderInfo {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, file.Close())
	}()

	reader, _, _, err := OpenReaderAt(file, 0, fileSize(t, path))
	require.NoError(t, err)
	header := reader.ReadHeader()
	reader.Close()
	return header
}

func expectedDocIDs(source testIndexSource, terms []string) []uint32 {
	if len(terms) == 0 {
		return nil
	}

	var result map[uint32]struct{}
	for _, term := range terms {
		postings, ok := source.terms[term8(term)]
		if !ok {
			return nil
		}
		current := make(map[uint32]struct{}, len(postings))
		for _, id := range postings {
			current[id] = struct{}{}
		}
		if result == nil {
			result = current
		} else {
			result = intersectDocSets(result, current)
		}
		if len(result) == 0 {
			return nil
		}
	}

	ids := make([]uint32, 0, len(result))
	for id := range result {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func expectedMergedIndex(sources []testIndexSource) testIndexSource {
	docs := make([]format.DocumentMetadata, 0)
	remaps := make([]map[uint32]uint32, len(sources))
	seenDocs := make(map[docTimeKey]uint32)

	for i, source := range sources {
		remap := make(map[uint32]uint32, len(source.docs))
		for _, doc := range source.docs {
			key := docTimeKey{min: doc.MinTimeUnix, max: doc.MaxTimeUnix}
			id, ok := seenDocs[key]
			if !ok {
				id = uint32(len(docs))
				seenDocs[key] = id
				docs = append(docs, format.DocumentMetadata{
					ID:          id,
					MinTimeUnix: doc.MinTimeUnix,
					MaxTimeUnix: doc.MaxTimeUnix,
				})
			}
			remap[doc.ID] = id
		}
		remaps[i] = remap
	}

	terms := make(map[[8]byte][]uint32)
	termSets := make(map[[8]byte]map[uint32]struct{})
	for i, source := range sources {
		for term, postings := range source.terms {
			docSet := termSets[term]
			if docSet == nil {
				docSet = make(map[uint32]struct{})
				termSets[term] = docSet
			}
			for _, id := range postings {
				docSet[remaps[i][id]] = struct{}{}
			}
		}
	}

	for term, docSet := range termSets {
		ids := make([]uint32, 0, len(docSet))
		for id := range docSet {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		terms[term] = ids
	}

	return testIndexSource{docs: docs, terms: terms}
}

func bitmapForDocIDs(ids []uint32) *roaring.Bitmap {
	bm := roaring.New()
	if len(ids) > 0 {
		bm.AddMany(ids)
	}
	return bm
}

func sortedTerms(terms map[[8]byte][]uint32) [][8]byte {
	keys := make([][8]byte, 0, len(terms))
	for term := range terms {
		keys = append(keys, term)
	}
	slices.SortFunc(keys, func(a, b [8]byte) int {
		return bytes.Compare(a[:], b[:])
	})
	return keys
}

func term8(s string) [8]byte {
	var term [8]byte
	copy(term[:], s)
	return term
}

func termString(term [8]byte) string {
	end := bytes.IndexByte(term[:], 0)
	if end == -1 {
		end = len(term)
	}
	return string(term[:end])
}

func intersectDocSets(left, right map[uint32]struct{}) map[uint32]struct{} {
	out := make(map[uint32]struct{})
	for id := range left {
		if _, ok := right[id]; ok {
			out[id] = struct{}{}
		}
	}
	return out
}

func buildComprehensiveSources() []testIndexSource {
	const (
		docsA       = 12
		docsB       = 16
		sharedDocs  = 4
		termsOnlyA  = 12
		termsOnlyB  = 15
		sharedTerms = 8
	)

	sourceA := testIndexSource{
		docs:  make([]format.DocumentMetadata, docsA),
		terms: make(map[[8]byte][]uint32),
	}
	sourceB := testIndexSource{
		docs:  make([]format.DocumentMetadata, docsB),
		terms: make(map[[8]byte][]uint32),
	}

	for i := range docsA {
		sourceA.docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: int64(i) * 1000,
			MaxTimeUnix: int64(i)*1000 + 500,
		}
	}

	for i := range docsB {
		minTime := int64(docsA+i) * 1000
		maxTime := minTime + 500
		if i < sharedDocs {
			minTime = sourceA.docs[i].MinTimeUnix
			maxTime = sourceA.docs[i].MaxTimeUnix
		}
		sourceB.docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: minTime,
			MaxTimeUnix: maxTime,
		}
	}

	for i := range termsOnlyA {
		key := term8(fmt.Sprintf("A%05d", i))
		sourceA.terms[key] = formulaDocIDs(docsA, i, 3, 5)
	}
	for i := range termsOnlyB {
		key := term8(fmt.Sprintf("B%05d", i))
		sourceB.terms[key] = formulaDocIDs(docsB, i, 4, 7)
	}
	for i := range sharedTerms {
		key := term8(fmt.Sprintf("S%05d", i))
		sourceA.terms[key] = formulaDocIDs(docsA, i+1, 2, 5)
		sourceB.terms[key] = formulaDocIDs(docsB, i+2, 3, 6)
	}

	sourceA.terms[term8("S00000")] = []uint32{0, 1, 4, 7}
	sourceA.terms[term8("S00001")] = []uint32{1, 4, 8}
	sourceB.terms[term8("S00000")] = []uint32{0, 3, 5, 9}
	sourceB.terms[term8("S00001")] = []uint32{0, 2, 5, 9}

	return []testIndexSource{sourceA, sourceB}
}

func formulaDocIDs(docCount, seed, modulus, fallbackMod int) []uint32 {
	ids := make([]uint32, 0)
	for i := range docCount {
		if (i+seed)%modulus == 0 || (i*seed+1)%fallbackMod == 0 {
			ids = append(ids, uint32(i))
		}
	}
	if len(ids) == 0 && docCount > 0 {
		ids = append(ids, uint32(seed%docCount))
	}
	return ids
}

func buildFuzzWriteCase(docSeed, termSeed, modeSeed uint8) (testIndexSource, [][]string) {
	mode := int(modeSeed % 7)
	docCount := int(docSeed%8) + 1
	termCount := int(termSeed%5) + 1

	switch mode {
	case 0:
		return testIndexSource{}, [][]string{nil}
	case 1:
		return testIndexSource{
			docs:  buildDocs(docCount),
			terms: map[[8]byte][]uint32{term8("M00000"): {0}},
		}, [][]string{{"M00000"}}
	case 2:
		return testIndexSource{
			docs:  buildDocs(docCount),
			terms: map[[8]byte][]uint32{term8("A00000"): buildSequentialIDs(docCount)},
		}, [][]string{{"A00000"}}
	case 3:
		source := testIndexSource{
			docs: buildDocs(max(docCount, 2)),
			terms: map[[8]byte][]uint32{
				term8("D00000"): {0},
				term8("D00001"): {1},
			},
		}
		return source, [][]string{
			{"D00000"},
			{"D00001"},
			{"D00000", "D00001"},
		}
	case 4:
		source := testIndexSource{
			docs: buildDocs(max(docCount, 3)),
			terms: map[[8]byte][]uint32{
				term8("O00000"): {0, 1},
				term8("O00001"): {1, 2},
			},
		}
		return source, [][]string{{"O00000", "O00001"}}
	case 5:
		source := testIndexSource{
			docs: buildDocs(max(docCount, 4)),
			terms: map[[8]byte][]uint32{
				term8("E00000"): {0, 1},
				term8("E00001"): {1, 2},
				term8("E00002"): {3},
			},
		}
		return source, [][]string{{"E00000", "E00001", "E00002"}}
	default:
		source := testIndexSource{
			docs:  buildDocs(max(docCount, 6)),
			terms: make(map[[8]byte][]uint32),
		}
		for i := 0; i < max(termCount, 4); i++ {
			source.terms[term8(fmt.Sprintf("F%05d", i))] = formulaDocIDs(len(source.docs), i+1, 2+i%3, 5+i%4)
		}
		return source, [][]string{
			{"F00000"},
			{"F00000", "F00001", "F00002"},
		}
	}
}

func buildFuzzMergeCase(sourceSeed, docSeed, modeSeed uint8) ([]testIndexSource, [][]string, bool) {
	mode := int(modeSeed % 7)
	docCount := int(docSeed%5) + 1
	sourceCount := int(sourceSeed%3) + 2

	switch mode {
	case 0:
		return []testIndexSource{{}, {}}, [][]string{nil}, false
	case 1:
		return []testIndexSource{
			{},
			{
				docs:  buildDocs(docCount),
				terms: map[[8]byte][]uint32{term8("N00000"): {0}},
			},
		}, [][]string{{"N00000"}}, false
	case 2:
		return []testIndexSource{
				{
					docs:  buildDocs(docCount),
					terms: map[[8]byte][]uint32{term8("A00000"): {0}},
				},
				{
					docs:  buildDocs(docCount),
					terms: map[[8]byte][]uint32{term8("B00000"): {0}},
				},
			}, [][]string{
				{"A00000"},
				{"B00000"},
			}, false
	case 3:
		return []testIndexSource{
			{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
					{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
				},
				terms: map[[8]byte][]uint32{term8("S00000"): {0, 1}},
			},
			{
				docs: []format.DocumentMetadata{
					{ID: 0, MinTimeUnix: 500, MaxTimeUnix: 600},
				},
				terms: map[[8]byte][]uint32{term8("S00000"): {0}},
			},
		}, [][]string{{"S00000"}}, false
	case 4:
		return []testIndexSource{
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 300, MaxTimeUnix: 400},
					},
					terms: map[[8]byte][]uint32{term8("F00000"): {0, 1}},
				},
				{
					docs: []format.DocumentMetadata{
						{ID: 0, MinTimeUnix: 100, MaxTimeUnix: 200},
						{ID: 1, MinTimeUnix: 500, MaxTimeUnix: 600},
					},
					terms: map[[8]byte][]uint32{
						term8("F00000"): {1},
						term8("B00000"): {0},
					},
				},
			}, [][]string{
				{"F00000"},
				{"B00000"},
			}, false
	case 5:
		sources := make([]testIndexSource, 0, sourceCount)
		for i := range sourceCount {
			source := testIndexSource{
				docs:  buildDocs(docCount + i),
				terms: make(map[[8]byte][]uint32),
			}
			for termIdx := range 3 {
				source.terms[term8(fmt.Sprintf("M%05d", termIdx))] = formulaDocIDs(len(source.docs), i+termIdx+1, 2+termIdx, 5+termIdx)
			}
			sources = append(sources, source)
		}
		return sources, [][]string{
			{"M00000"},
			{"M00000", "M00001"},
		}, false
	default:
		return buildComprehensiveSources(), [][]string{{"S00000", "S00001"}}, true
	}
}

func buildDocs(count int) []format.DocumentMetadata {
	docs := make([]format.DocumentMetadata, count)
	for i := range count {
		docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: int64(i) * 100,
			MaxTimeUnix: int64(i)*100 + 50,
		}
	}
	return docs
}

func buildSequentialIDs(count int) []uint32 {
	ids := make([]uint32, count)
	for i := range count {
		ids[i] = uint32(i)
	}
	return ids
}
