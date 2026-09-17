package v3

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestStreamingIndexWriter_CleanupOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.idx")

	w, err := newStreamingIndexWriter(path, DefaultFastIndexWriteConfig(), 1)
	require.NoError(t, err)
	w.AddDocument(format.DocumentMetadata{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2})

	// Inject an error so Close short-circuits and removes the partial file.
	w.err = fmt.Errorf("injected error")

	err = w.Close()
	require.Error(t, err)

	// The output file must not exist after Close returns an error.
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestStreamingMerge_FromFiles verifies that the streaming merge produces a
// valid merged index from multiple file-backed sources with overlapping
// documents.
func TestStreamingMerge_FromFiles(t *testing.T) {
	const numSources = 4
	const docsPerSource = 500
	const termsPerSource = 2000

	dir := t.TempDir()
	inputPaths := make([]string, numSources)

	for s := range numSources {
		docs := make([]format.DocumentMetadata, docsPerSource)
		for i := range docs {
			// ~50% overlap: docs 0-249 shared across sources.
			baseTime := int64((s/2)*100 + i)
			docs[i] = format.DocumentMetadata{
				ID:          uint32(i),
				MinTimeUnix: baseTime,
				MaxTimeUnix: baseTime + 50,
			}
		}

		termPostings := make(map[[8]byte][]uint32)
		for j := range termsPerSource {
			var tk [8]byte
			// "T" + 1-digit source + 4-digit term = 6 chars (fits NgramLength).
			copy(tk[:], fmt.Sprintf("T%d%04d", s, j))
			var ids []uint32
			for d := range docsPerSource {
				if (j*31+d*17)%5 == 0 {
					ids = append(ids, uint32(d))
				}
			}
			if len(ids) == 0 {
				ids = []uint32{uint32(j % docsPerSource)}
			}
			termPostings[tk] = ids
		}

		inputPaths[s] = writeTestIndex(t, dir, fmt.Sprintf("src%d.idx", s), docs, termPostings)
	}

	cfg := DefaultFastIndexWriteConfig()

	streamOut := filepath.Join(dir, "stream_merged.idx")
	streamOutFile, err := os.Create(streamOut)
	require.NoError(t, err)
	_, err = mergeFilesTo(context.Background(), t, inputPaths, streamOutFile, cfg)
	require.NoError(t, err)
	require.NoError(t, streamOutFile.Close())

	rs, err := OpenIndexFile(streamOut)
	require.NoError(t, err)
	defer rs.Close()

	require.True(t, rs.Header().TermCount > 0, "merged index should have terms")
	require.True(t, rs.Header().DocumentCount > 0, "merged index should have documents")

	it, err := rs.NewTermIterator()
	require.NoError(t, err)
	termCount := uint64(0)
	for it.Next() {
		termCount++
		require.False(t, it.Bitmap().MatchesAll, "all terms should have bitmaps (density filter disabled)")
	}
	require.NoError(t, it.Err())
	require.Equal(t, rs.Header().TermCount, termCount)
}

// --- shared test helpers ---

// openTestReaderAt opens a file for reading as io.ReaderAt and returns it with
// its size. Registers a Cleanup that closes the file when the test ends.
func openTestReaderAt(tb testing.TB, path string) (*os.File, int64) {
	tb.Helper()
	f, err := os.Open(path)
	require.NoError(tb, err)
	tb.Cleanup(func() { f.Close() })
	fi, err := f.Stat()
	require.NoError(tb, err)
	return f, fi.Size()
}

// mergeFilesTo opens inputPaths as io.ReaderAts and streams the merged output
// to out via StreamingMergeIndexReaders. This is the file-path analogue of
// the reader-based merge API; it exists to avoid boilerplate in the ~8 tests
// and benchmarks that produce fixtures on disk and want to merge them.
// Production compaction always goes through StreamingMergeIndexReaders
// directly with io.ReaderAts backed by object-storage range reads.
func mergeFilesTo(ctx context.Context, tb testing.TB, inputPaths []string, out io.Writer, cfg IndexWriteConfig) (format.HeaderInfo, error) {
	tb.Helper()
	readers := make([]io.ReaderAt, len(inputPaths))
	sizes := make([]int64, len(inputPaths))
	for i, p := range inputPaths {
		f, size := openTestReaderAt(tb, p)
		readers[i] = f
		sizes[i] = size
	}
	return StreamingMergeIndexReaders(ctx, readers, sizes, out, cfg)
}
