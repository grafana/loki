package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

// columnReaderTestStrings contains enough strings to span multiple pages
var columnReaderTestStrings = []string{
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
}

func Test_columnReader_ReadAll(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)
	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)
	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_SeekAcrossPages(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	// Find a position near the end of the first page
	endFirstPage := col.Pages[0].PageDesc().RowCount - 2

	cr := newColumnReader(col)
	_, err := cr.Seek(int64(endFirstPage), io.SeekStart)
	require.NoError(t, err)

	// Read enough values to span into the second page
	arr, err := cr.Read(t.Context(), &alloc, 4)
	require.NoError(t, err)
	require.Equal(t, 4, arr.Len())

	actual := convertToStrings(t, arr)
	expected := columnReaderTestStrings[endFirstPage : endFirstPage+4]
	require.Equal(t, expected, actual)
}

func Test_columnReader_SeekToStart(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	// Seek back to start and read again
	_, err = cr.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_Reset(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	// Reset and read again
	cr.Reset(col)

	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	require.Equal(t, columnReaderTestStrings, actual)
}

// buildMultiPageColumn creates a column with multiple pages by using a small page size
func buildMultiPageColumn(t *testing.T, values []string) *MemColumn {
	t.Helper()

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 128, // Small page size to force multiple pages
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	for i, v := range values {
		require.NoError(t, builder.Append(i, BinaryValue([]byte(v))))
	}

	col, err := builder.Flush()
	require.NoError(t, err)
	return col
}

func readColumn(t *testing.T, alloc *memory.Allocator, cr *columnReader, batchSize int) ([]string, error) {
	t.Helper()

	// Temporary allocator for the intermediate arrays.
	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	var all []string

	for {
		// Memory doesn't survive past the loop so we can eagerly reclaim.
		tempAlloc.Reclaim()

		arr, err := cr.Read(context.Background(), tempAlloc, batchSize)
		if arr != nil {
			all = append(all, convertToStrings(t, arr)...)
		}
		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}
