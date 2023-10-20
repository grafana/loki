package v1

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
)

func TestArchive(t *testing.T) {
	// for writing files to two dirs for comparison and ensuring they're equal
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	numSeries := 100
	numKeysPerSeries := 10000
	data, _ := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		NewDirectoryBlockWriter(dir1),
	)

	require.Nil(t, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	require.Nil(t, builder.BuildFrom(itr))

	reader := NewDirectoryBlockReader(dir1)

	w := bytes.NewBuffer(nil)
	require.Nil(t, TarGz(w, reader))

	require.Nil(t, UnTarGz(dir2, w))

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
