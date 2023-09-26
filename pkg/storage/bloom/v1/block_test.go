package v1

import (
	"bytes"
	"testing"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/util/encoding"
	"github.com/stretchr/testify/require"
)

func TestBlockEncoding(t *testing.T) {

	pages, _ := mkBasicSeriesPages(2, 100, 0, 0xffff, 0, 100)
	src := NewBlockIndex(chunkenc.EncSnappy)

	buf := bytes.NewBuffer(nil)

	_, err := src.WriteTo(NewSliceIter(pages), buf)
	require.Nil(t, err)

	data := buf.Bytes()
	b := NewBlock(NewByteReader(data))
	require.Nil(t, b.LoadHeaders())
	require.Equal(t, src, b.index)

	for i, header := range b.index.pageHeaders {
		var page SeriesPage
		decoder := encoding.DecWith(data[header.Offset : header.Offset+header.Len])
		require.Nil(t, page.Decode(&decoder, chunkenc.GetReaderPool(b.index.schema.encoding), header.DecompressedLen))
		require.Equal(t, pages[i], page)
	}
}

func TestSeriesIter(t *testing.T) {
	pages, series := mkBasicSeriesPages(2, 100, 0, 0xffff, 0, 100)
	src := NewBlockIndex(chunkenc.EncSnappy)

	buf := bytes.NewBuffer(nil)

	_, err := src.WriteTo(NewSliceIter(pages), buf)
	require.Nil(t, err)

	data := buf.Bytes()
	b := NewBlock(NewByteReader(data))
	itr := b.Series()

	for i := range series {
		require.True(t, itr.Next())
		exp, got := series[i], itr.At().Series
		require.Equal(t, exp, got)
		require.Nil(t, itr.Err())
	}

	require.Equal(t, false, itr.Next())
	require.Nil(t, itr.Err())

}
