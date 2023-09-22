package v1

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlockEncoding(t *testing.T) {

	pages := mkBasicSeriesPages(2, 100, 0, 0xffff, 0, 100)
	src := NewBlockIndex()

	buf := bytes.NewBuffer(nil)

	_, err := src.WriteTo(NewSliceIter(pages), buf)
	require.Nil(t, err)

	var b Block
	require.Nil(t, b.LoadHeaders(buf.Bytes()))
	require.Equal(t, src, b.index)
}
