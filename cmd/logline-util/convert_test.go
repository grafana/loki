package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestConvertIndex_V5KeepsDocumentLayout(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.lidx")
	dst := filepath.Join(dir, "dst.lidx")
	createTestV5File(t, src, 3, 64, 65)

	_, err := convertIndex(src, dst, "v5", 0, format.PostingsEncodingFastEliasFanoBlocked, logline.PostingsEncodingEliasFano)
	require.NoError(t, err)

	reader, version, err := logline.OpenFile(dst)
	require.NoError(t, err)
	defer reader.Close()
	require.Equal(t, "v5", version)
	layout, ok := logline.DocumentLayout(reader)
	require.True(t, ok)
	require.Equal(t, format.DocumentLayout{
		Interval:  16 * time.Second,
		ShardBits: 5,
		DayStart:  time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	}, layout)

	it, err := reader.NewTermIterator()
	require.NoError(t, err)
	require.True(t, it.Next())
	require.Equal(t, []uint32{3, 64, 65}, it.Bitmap().Roaring.ToArray())
	require.False(t, it.Next())
	require.NoError(t, it.Err())
}

func TestConvertIndex_RefusesChangingDocumentNumbering(t *testing.T) {
	dir := t.TempDir()
	v3File := filepath.Join(dir, "v3.lidx")
	v5File := filepath.Join(dir, "v5.lidx")
	createTestLintFile(t, v3File)
	createTestV5File(t, v5File, 3)

	_, err := convertIndex(v3File, filepath.Join(dir, "out-v5.lidx"), "v5", 0, format.PostingsEncodingFastDeltaVarIntBlocked, logline.PostingsEncodingDeltaVarInt)
	require.ErrorContains(t, err, "cannot convert a v3 index to v5: its document IDs are not cells of a day")

	_, err = convertIndex(v5File, filepath.Join(dir, "out-v3.lidx"), "v3", 0, format.PostingsEncodingFastDeltaVarIntBlocked, logline.PostingsEncodingDeltaVarInt)
	require.ErrorContains(t, err, "cannot convert a v5 index to v3: it has no document table")
}
