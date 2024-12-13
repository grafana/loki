package v1

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
)

func TestArchive(t *testing.T) {
	t.Parallel()
	// for writing files to two dirs for comparison and ensuring they're equal
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	numSeries := 100
	data, _ := MkBasicSeriesWithBlooms(numSeries, 0x0000, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			Schema:         NewSchema(CurrentSchemaVersion, compression.None),
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		NewDirectoryBlockWriter(dir1),
	)

	require.Nil(t, err)
	itr := v2.NewSliceIter(data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)

	reader := NewDirectoryBlockReader(dir1)

	w := bytes.NewBuffer(nil)
	require.Nil(t, Tar(w, reader))

	require.Nil(t, UnTar(dir2, w))

	reader2 := NewDirectoryBlockReader(dir2)

	// Check Index is byte for byte equivalent
	srcIndex, err := reader.Index()
	require.Nil(t, err)
	_, err = srcIndex.Seek(0, io.SeekStart)
	require.Nil(t, err)
	dstIndex, err := reader2.Index()
	require.Nil(t, err)
	_, err = dstIndex.Seek(0, io.SeekStart)
	require.Nil(t, err)

	srcIndexBytes, err := io.ReadAll(srcIndex)
	require.Nil(t, err)
	dstIndexBytes, err := io.ReadAll(dstIndex)
	require.Nil(t, err)
	require.Equal(t, srcIndexBytes, dstIndexBytes)

	// Check Blooms is byte for byte equivalent
	srcBlooms, err := reader.Blooms()
	require.Nil(t, err)
	_, err = srcBlooms.Seek(0, io.SeekStart)
	require.Nil(t, err)
	dstBlooms, err := reader2.Blooms()
	require.Nil(t, err)
	_, err = dstBlooms.Seek(0, io.SeekStart)
	require.Nil(t, err)

	srcBloomsBytes, err := io.ReadAll(srcBlooms)
	require.Nil(t, err)
	dstBloomsBytes, err := io.ReadAll(dstBlooms)
	require.Nil(t, err)
	require.Equal(t, srcBloomsBytes, dstBloomsBytes)
}

func TestArchiveCompression(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		enc compression.Codec
	}{
		{compression.None},
		{compression.GZIP},
		{compression.Snappy},
		{compression.LZ4_64k},
		{compression.LZ4_256k},
		{compression.LZ4_1M},
		{compression.LZ4_4M},
		{compression.Flate},
		{compression.Zstd},
	} {
		t.Run(tc.enc.String(), func(t *testing.T) {
			// for writing files to two dirs for comparison and ensuring they're equal
			dir1 := t.TempDir()
			dir2 := t.TempDir()

			numSeries := 100
			data, _ := MkBasicSeriesWithBlooms(numSeries, 0x0000, 0xffff, 0, 10000)

			builder, err := NewBlockBuilder(
				BlockOptions{
					Schema:         NewSchema(CurrentSchemaVersion, compression.None),
					SeriesPageSize: 100,
					BloomPageSize:  10 << 10,
				},
				NewDirectoryBlockWriter(dir1),
			)

			require.Nil(t, err)
			itr := v2.NewSliceIter(data)
			_, err = builder.BuildFrom(itr)
			require.Nil(t, err)

			reader := NewDirectoryBlockReader(dir1)

			w := bytes.NewBuffer(nil)
			require.Nil(t, TarCompress(tc.enc, w, reader))

			require.Nil(t, UnTarCompress(tc.enc, dir2, w))

			reader2 := NewDirectoryBlockReader(dir2)

			// Check Index is byte for byte equivalent
			srcIndex, err := reader.Index()
			require.Nil(t, err)
			_, err = srcIndex.Seek(0, io.SeekStart)
			require.Nil(t, err)
			dstIndex, err := reader2.Index()
			require.Nil(t, err)
			_, err = dstIndex.Seek(0, io.SeekStart)
			require.Nil(t, err)

			srcIndexBytes, err := io.ReadAll(srcIndex)
			require.Nil(t, err)
			dstIndexBytes, err := io.ReadAll(dstIndex)
			require.Nil(t, err)
			require.Equal(t, srcIndexBytes, dstIndexBytes)

			// Check Blooms is byte for byte equivalent
			srcBlooms, err := reader.Blooms()
			require.Nil(t, err)
			_, err = srcBlooms.Seek(0, io.SeekStart)
			require.Nil(t, err)
			dstBlooms, err := reader2.Blooms()
			require.Nil(t, err)
			_, err = dstBlooms.Seek(0, io.SeekStart)
			require.Nil(t, err)

			srcBloomsBytes, err := io.ReadAll(srcBlooms)
			require.Nil(t, err)
			dstBloomsBytes, err := io.ReadAll(dstBlooms)
			require.Nil(t, err)
			require.Equal(t, srcBloomsBytes, dstBloomsBytes)

		})
	}
}
