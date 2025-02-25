package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
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
	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)
	actualValues, err := readColumn(cr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_SeekAcrossPages(t *testing.T) {
	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	// Find a position near the end of the first page
	endFirstPage := col.Pages[0].PageInfo().RowCount - 2

	cr := newColumnReader(col)
	_, err := cr.Seek(int64(endFirstPage), io.SeekStart)
	require.NoError(t, err)

	// Read enough values to span into the second page
	batch := make([]Value, 4)
	n, err := cr.Read(context.Background(), batch)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	actual := convertToStrings(t, batch[:n])
	expected := columnReaderTestStrings[endFirstPage : endFirstPage+4]
	require.Equal(t, expected, actual)
}

func Test_columnReader_SeekToStart(t *testing.T) {
	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(cr, 4)
	require.NoError(t, err)

	// Seek back to start and read again
	_, err = cr.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actualValues, err := readColumn(cr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_Reset(t *testing.T) {
	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(cr, 4)
	require.NoError(t, err)

	// Reset and read again
	cr.Reset(col)

	actualValues, err := readColumn(cr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, columnReaderTestStrings, actual)
}

// buildMultiPageColumn creates a column with multiple pages by using a small page size
func buildMultiPageColumn(t *testing.T, values []string) *MemColumn {
	t.Helper()

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 128, // Small page size to force multiple pages
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	for i, v := range values {
		require.NoError(t, builder.Append(i, StringValue(v)))
	}

	col, err := builder.Flush()
	require.NoError(t, err)
	return col
}

func readColumn(cr *columnReader, batchSize int) ([]Value, error) {
	var (
		all []Value

		batch = make([]Value, batchSize)
	)

	for {
		n, err := cr.Read(context.Background(), batch)
		if n > 0 {
			all = append(all, batch[:n]...)
		}
		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}
