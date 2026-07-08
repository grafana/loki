package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// buildStreamReaderFixture writes a small index into a temp dir and returns
// its path. Series are inserted in fingerprint order (Creator requires this).
// Kept as a helper so future streaming-reader tests can share the same setup.
func buildStreamReaderFixture(t *testing.T, version int) string {
	t.Helper()

	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)

	iw, err := NewWriter(context.Background(), version, fn)
	require.NoError(t, err)

	series := []labels.Labels{
		labels.FromStrings("a", "1", "b", "1"),
		labels.FromStrings("a", "1", "b", "2"),
		labels.FromStrings("a", "1", "b", "3"),
		labels.FromStrings("a", "2", "b", "1"),
		labels.FromStrings("a", "2", "b", "2"),
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})

	for _, s := range []string{"1", "2", "3", "a", "b"} {
		require.NoError(t, iw.AddSymbol(s))
	}
	for i, s := range series {
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i+1), s, model.Fingerprint(labels.StableHash(s))))
	}

	_, err = iw.Close(false)
	require.NoError(t, err)
	return fn
}

// TestStreamReader_MetadataMatchesMmap covers Phase 2 Bucket A proposals
// P2.A1 + P2.A2: the streaming reader must open, expose file format
// version, and return the same TOC-derived bounds/checksum as the mmap
// reader. Query surface methods are covered by later tests as they land.
func TestStreamReader_MetadataMatchesMmap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
	}{
		{"V3", FormatV3},
		{"V4", FormatV4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildStreamReaderFixture(t, tc.version)

			mmap, err := NewFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = mmap.Close() })

			stream, err := NewStreamFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = stream.Close() })

			require.Equal(t, mmap.Version(), stream.Version(), "version")
			require.Equal(t, mmap.Checksum(), stream.Checksum(), "checksum")

			mFrom, mThrough := mmap.Bounds()
			sFrom, sThrough := stream.Bounds()
			require.Equal(t, mFrom, sFrom, "bounds from")
			require.Equal(t, mThrough, sThrough, "bounds through")
		})
	}
}

// TestStreamReader_RejectsCorruptMagic ensures header validation runs.
func TestStreamReader_RejectsCorruptMagic(t *testing.T) {
	path := buildStreamReaderFixture(t, FormatV3)

	// Verify open works on a healthy fixture first.
	r, err := NewStreamFileReader(path)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	// Stomp the magic bytes.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 0)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = NewStreamFileReader(path)
	require.Error(t, err)
}
