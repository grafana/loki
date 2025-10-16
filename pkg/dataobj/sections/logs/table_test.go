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

var (
	pageSize = 1024
	pageRows = 0
)

func Test_table_metadataCleanup(t *testing.T) {
	var buf tableBuffer
	initBuffer(&buf)

	_ = buf.Metadata("foo", pageSize, pageRows, dataset.CompressionOptions{})
	_ = buf.Metadata("bar", pageSize, pageRows, dataset.CompressionOptions{})

	table, err := buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 2, len(table.Metadatas))

	initBuffer(&buf)
	_ = buf.Metadata("bar", pageSize, pageRows, dataset.CompressionOptions{})

	table, err = buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 1, len(table.Metadatas))
	require.Equal(t, "bar", table.Metadatas[0].Desc.Tag)
}

func initBuffer(buf *tableBuffer) {
	buf.StreamID(pageSize, pageRows)
	buf.Timestamp(pageSize, pageRows)
	buf.Message(pageSize, pageRows, dataset.CompressionOptions{})
}

func Test_mergeTables(t *testing.T) {
	// Test data - duplicates are added to ensure the resulting objects are deduplicated if all attributes match.
	testRecords := struct {
		tableA []Record
		tableB []Record
		tableC []Record
	}{
		tableA: []Record{
			{StreamID: 3, Timestamp: time.Unix(3, 0), Line: []byte("hello")},
			{StreamID: 2, Timestamp: time.Unix(2, 0), Line: []byte("how")},
			{StreamID: 1, Timestamp: time.Unix(1, 0), Line: []byte("you")},
		},
		tableB: []Record{
			{StreamID: 3, Timestamp: time.Unix(3, 0), Line: []byte("hello")}, // Duplicate in tableA
			{StreamID: 1, Timestamp: time.Unix(2, 0), Line: []byte("world")},
			{StreamID: 3, Timestamp: time.Unix(1, 0), Line: []byte("goodbye")},
		},
		tableC: []Record{
			{StreamID: 3, Timestamp: time.Unix(2, 0), Line: []byte("are")},
			{StreamID: 3, Timestamp: time.Unix(2, 0), Line: []byte("are")}, // Duplicate within tableC
			{StreamID: 2, Timestamp: time.Unix(1, 0), Line: []byte("doing?")},
		},
	}

	// Test both sort strategies
	sortStrategies := []struct {
		name      string
		sortOrder SortOrder
		expected  string
	}{
		{
			name:      "SortTimestampDESC",
			sortOrder: SortTimestampDESC,
			expected:  "hello world how are you doing? goodbye",
		},
		{
			name:      "SortStreamASC",
			sortOrder: SortStreamASC,
			expected:  "world you how doing? hello are goodbye",
		},
	}

	for _, strategy := range sortStrategies {
		t.Run(strategy.name, func(t *testing.T) {
			var buf tableBuffer

			// Build tables with sort strategy
			tableA := buildTable(&buf, pageSize, pageRows, dataset.CompressionOptions{}, testRecords.tableA, strategy.sortOrder)
			tableB := buildTable(&buf, pageSize, pageRows, dataset.CompressionOptions{}, testRecords.tableB, strategy.sortOrder)
			tableC := buildTable(&buf, pageSize, pageRows, dataset.CompressionOptions{}, testRecords.tableC, strategy.sortOrder)

			// TableC should have been initially deduped by buildTable
			require.Equal(t, tableC.Timestamp.Desc.RowsCount, 2)

			mergedTable, err := mergeTables(&buf, pageSize, pageRows, dataset.CompressionOptions{}, []*table{tableA, tableB, tableC}, strategy.sortOrder)
			require.NoError(t, err)

			mergedColumns, err := result.Collect(mergedTable.ListColumns(context.Background()))
			require.NoError(t, err)

			var actual []string

			r := dataset.NewReader(dataset.ReaderOptions{
				Dataset: mergedTable,
				Columns: mergedColumns,
			})

			rows := make([]dataset.Row, pageSize)

			for {
				n, err := r.Read(context.Background(), rows)
				if err != nil && !errors.Is(err, io.EOF) {
					require.NoError(t, err)
				} else if n == 0 && errors.Is(err, io.EOF) {
					break
				}

				for _, row := range rows[:n] {
					require.Len(t, row.Values, 3)
					require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, row.Values[2].Type())

					actual = append(actual, string(row.Values[2].Binary()))
				}
			}

			require.Equal(t, strategy.expected, strings.Join(actual, " "))
		})
	}
}
