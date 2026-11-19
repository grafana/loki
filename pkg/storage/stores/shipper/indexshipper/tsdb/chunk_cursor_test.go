package tsdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

func TestFileChunkCursorSurvivesIndexRemoval(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    index.ReaderOptions
		version int
	}{
		{name: "mmap", opts: index.MmapOptions{}, version: index.FormatV3},
		// Force the streaming reader to reopen by path unless it pins a handle.
		{name: "stream v2", opts: index.StreamOptions{}, version: index.FormatV2},
		{name: "stream v3", opts: index.StreamOptions{}, version: index.FormatV3},
		{name: "stream v4", opts: index.StreamOptions{}, version: index.FormatV4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testFileChunkCursorSurvivesIndexRemoval(t, tc.opts, tc.version)
		})
	}
}

func testFileChunkCursorSurvivesIndexRemoval(t *testing.T, opts index.ReaderOptions, version int) {
	t.Helper()
	dir := t.TempDir()
	builder := NewBuilder(version)
	chunks := []index.ChunkMeta{
		{MinTime: 40, MaxTime: 50, Checksum: 4},
		{MinTime: 10, MaxTime: 20, Checksum: 1},
		{MinTime: 30, MaxTime: 40, Checksum: 3},
	}
	builder.AddSeries(labels.FromStrings(TenantLabel, "synthetic-a", "app", "test"), 5, chunks)
	// A second series with fingerprint 5 starts before the first one ends.
	builder.AddSeries(labels.FromStrings(TenantLabel, "synthetic-a", "app", "test", "instance", "two", "variant", "collision"), 5, []index.ChunkMeta{
		{MinTime: 20, MaxTime: 30, Checksum: 2},
	})
	builder.AddSeries(labels.FromStrings(TenantLabel, "synthetic-b", "app", "test"), 7, chunks)
	builder.AddSeries(labels.FromStrings(TenantLabel, "synthetic-a", "app", "test", "instance", "two"), 9, chunks)
	id, err := builder.Build(t.Context(), dir, func(from, through model.Time, checksum uint32) Identifier {
		return NewPrefixedIdentifier(SingleTenantTSDBIdentifier{
			TS: time.Unix(1, 0), From: from, Through: through, Checksum: checksum,
		}, dir, dir)
	})
	require.NoError(t, err)
	file, err := NewShippableTSDBFile(id, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	// Two files exercise the worker context canceled at the end of IndexSlice.For.
	common := NewMultiTenantIndex(file)
	client := NewIndexClient(NewMultiIndex(IndexSlice{common, common}), IndexClientOptions{}, nil)
	empty, err := client.OpenChunkCursors(t.Context(), "synthetic-a", 15, 45, labels.MustNewMatcher(labels.MatchEqual, "app", "missing"))
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Zero(t, file.leases)

	gap, err := openChunkCursors(t.Context(), common, "synthetic-a", 21, 29, nil,
		labels.MustNewMatcher(labels.MatchEqual, "instance", "two"),
		labels.MustNewMatcher(labels.MatchEqual, "variant", ""),
	)
	require.NoError(t, err)
	require.Len(t, gap, 1)
	t.Cleanup(func() { require.NoError(t, sharding.CloseChunkCursors(gap)) })
	gapCursor := gap[0].(*fileChunkCursor)
	require.Zero(t, cap(gapCursor.chunks))
	require.False(t, gapCursor.Next())
	require.NoError(t, gapCursor.Err())
	require.Zero(t, cap(gapCursor.chunks))
	require.NoError(t, gapCursor.Close())

	cursors, err := client.OpenChunkCursors(t.Context(), "synthetic-a", 15, 45, labels.MustNewMatcher(labels.MatchEqual, "app", "test"))
	require.NoError(t, err)
	require.Len(t, cursors, 2)
	t.Cleanup(func() { require.NoError(t, sharding.CloseChunkCursors(cursors)) })

	file.SetChunkFilterer(onlySecondInstance{})
	filtered, err := client.OpenChunkCursors(t.Context(), "synthetic-a", 15, 45, labels.MustNewMatcher(labels.MatchEqual, "app", "test"))
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	t.Cleanup(func() { require.NoError(t, sharding.CloseChunkCursors(filtered)) })

	require.NoError(t, file.Close())
	require.NoError(t, os.Remove(id.Path()))
	_, err = client.OpenChunkCursors(t.Context(), "synthetic-a", 15, 45, labels.MustNewMatcher(labels.MatchEqual, "app", "test"))
	require.ErrorIs(t, err, sharding.ErrChunkCursorsUnsupported)

	// Read the first label-filtered series after unlinking, while its label
	// value symbols are still cold in the streaming reader's cache.
	for _, cursor := range filtered {
		var fingerprints []uint64
		for cursor.Next() {
			fingerprints = append(fingerprints, cursor.At().Fingerprint)
		}
		require.NoError(t, cursor.Err())
		require.Equal(t, []uint64{5, 9, 9, 9}, fingerprints)
	}
	require.NoError(t, sharding.CloseChunkCursors(filtered))

	for _, cursor := range cursors {
		var fingerprints []uint64
		var checksums []uint32
		for cursor.Next() {
			ref := cursor.At()
			require.Equal(t, "synthetic-a", ref.UserID)
			fingerprints = append(fingerprints, ref.Fingerprint)
			checksums = append(checksums, ref.Checksum)
		}
		require.NoError(t, cursor.Err())
		require.Equal(t, []uint64{5, 5, 5, 5, 9, 9, 9}, fingerprints)
		require.Equal(t, []uint32{1, 2, 3, 4, 1, 3, 4}, checksums)
	}
	require.NoError(t, sharding.CloseChunkCursors(cursors))
	require.Zero(t, file.leases)
}

type onlySecondInstance struct{}

func (f onlySecondInstance) ForRequest(context.Context) chunk.Filterer { return f }
func (onlySecondInstance) ShouldFilter(ls labels.Labels) bool          { return ls.Get("instance") != "two" }
func (onlySecondInstance) RequiredLabelNames() []string                { return []string{"instance"} }
func (onlySecondInstance) PushableLabelMatchers() []*labels.Matcher    { return nil }
