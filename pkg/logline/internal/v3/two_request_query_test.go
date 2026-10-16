package v3

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

type trackedRead struct {
	Offset int64
	Length int
}

type trackingReaderAt struct {
	data  []byte
	mu    sync.Mutex
	reads []trackedRead
}

func newTrackingReaderAt(data []byte) *trackingReaderAt {
	return &trackingReaderAt{data: data}
}

func (r *trackingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	r.reads = append(r.reads, trackedRead{Offset: off, Length: len(p)})
	r.mu.Unlock()

	if off < 0 || off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *trackingReaderAt) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = r.reads[:0]
}

func (r *trackingReaderAt) Reads() []trackedRead {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]trackedRead, len(r.reads))
	copy(out, r.reads)
	return out
}

func TestTwoRequestQuery(t *testing.T) {
	type testCase struct {
		name   string
		config IndexWriteConfig
	}

	cases := []testCase{
		{
			name:   "fast_deltavarint",
			config: DefaultFastIndexWriteConfig(),
		},
	}

	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			data, queryTerm, expectedDocIDs := buildQueryTestIndex(t, tc.config)

			tracker := newTrackingReaderAt(data)
			index, err := OpenIndexAt(tracker, 0, int64(len(data)))
			require.NoError(t, err)
			defer index.Close()

			// Simulate header/directory cache warmup.
			tracker.Reset()

			got, err := index.query(queryTerm)
			require.NoError(t, err)
			require.Equal(t, expectedDocIDs, got)

			reads := tracker.Reads()
			require.Len(t, reads, 2, "query should perform exactly 2 object reads after warmup")
			for _, read := range reads {
				require.LessOrEqual(t, read.Length, format.MaxQueryRequestBytes, "single query read must stay <= 8MB")
			}
		})
	}
}

func buildQueryTestIndex(t *testing.T, cfg IndexWriteConfig) ([]byte, string, []uint32) {
	t.Helper()

	const (
		docCount  = 20
		termCount = 300
	)

	path := fmt.Sprintf("%s/test-%d.lidx", t.TempDir(), os.Getpid())
	cfg.DensityThreshold = 0 // disable density filter for test
	writer, err := newStreamingIndexWriter(path, cfg, docCount)
	require.NoError(t, err)

	docs := make([]format.DocumentMetadata, 0, docCount)
	for i := range docCount {
		docs = append(docs, format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: int64(i),
			MaxTimeUnix: int64(i),
		})
	}
	writer.AddDocuments(docs)

	// Terms formatted as T%05d sort lexicographically, matching StreamingIndexWriter's
	// ascending order requirement.
	terms := make([]string, 0, termCount)
	expected := make(map[string][]uint32, termCount)
	for i := range termCount {
		term := fmt.Sprintf("T%05d", i)
		terms = append(terms, term)

		docIDs := []uint32{
			uint32(i % docCount),
			uint32((i * 7) % docCount),
			uint32((i * 13) % docCount),
		}
		docIDs = uniqueSorted(docIDs)
		expected[term] = docIDs

		var key [8]byte
		copy(key[:], term)
		bm := roaring.New()
		bm.AddMany(docIDs)
		require.NoError(t, writer.WriteTermBitmap(key, format.Bitmap{Roaring: bm}))
	}
	require.NoError(t, writer.Close())

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	queryTerm := terms[173]
	return content, queryTerm, expected[queryTerm]
}

func uniqueSorted(values []uint32) []uint32 {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	out := values[:1]
	for i := 1; i < len(values); i++ {
		if values[i] != out[len(out)-1] {
			out = append(out, values[i])
		}
	}
	return out
}
