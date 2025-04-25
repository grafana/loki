package logs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

func Test_table_metadataCleanup(t *testing.T) {
	var buf tableBuffer
	initBuffer(&buf)

	_ = buf.Metadata("foo", 1024, dataset.CompressionOptions{})
	_ = buf.Metadata("bar", 1024, dataset.CompressionOptions{})

	table, err := buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 2, len(table.Metadatas))

	initBuffer(&buf)
	_ = buf.Metadata("bar", 1024, dataset.CompressionOptions{})

	table, err = buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 1, len(table.Metadatas))
	require.Equal(t, "bar", table.Metadatas[0].Info.Name)
}

func initBuffer(buf *tableBuffer) {
	buf.StreamID(1024)
	buf.Timestamp(1024)
	buf.Message(1024, dataset.CompressionOptions{})
}

func Test_mergeTables(t *testing.T) {
	var buf tableBuffer

	var (
		tableA = buildTable(&buf, 1024, dataset.CompressionOptions{}, []Record{
			{StreamID: 1, Timestamp: time.Unix(1, 0), Line: []byte("hello")},
			{StreamID: 2, Timestamp: time.Unix(2, 0), Line: []byte("are")},
			{StreamID: 3, Timestamp: time.Unix(3, 0), Line: []byte("goodbye")},
		})

		tableB = buildTable(&buf, 1024, dataset.CompressionOptions{}, []Record{
			{StreamID: 1, Timestamp: time.Unix(2, 0), Line: []byte("world")},
			{StreamID: 3, Timestamp: time.Unix(1, 0), Line: []byte("you")},
		})

		tableC = buildTable(&buf, 1024, dataset.CompressionOptions{}, []Record{
			{StreamID: 2, Timestamp: time.Unix(1, 0), Line: []byte("how")},
			{StreamID: 3, Timestamp: time.Unix(2, 0), Line: []byte("doing?")},
		})
	)

	mergedTable, err := mergeTables(&buf, 1024, dataset.CompressionOptions{}, []*table{tableA, tableB, tableC})
	require.NoError(t, err)

	mergedColumns, err := result.Collect(mergedTable.ListColumns(context.Background()))
	require.NoError(t, err)

	var actual []string

	r := dataset.NewReader(dataset.ReaderOptions{
		Dataset: mergedTable,
		Columns: mergedColumns,
	})

	rows := make([]dataset.Row, 1024)

	for {
		n, err := r.Read(context.Background(), rows)
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		} else if n == 0 && errors.Is(err, io.EOF) {
			break
		}

		for _, row := range rows[:n] {
			require.Len(t, row.Values, 3)
			require.Equal(t, datasetmd.VALUE_TYPE_BYTE_ARRAY, row.Values[2].Type())

			actual = append(actual, string(row.Values[2].ByteArray()))
		}
	}

	require.Equal(t, "hello world how are you doing? goodbye", strings.Join(actual, " "))
}
