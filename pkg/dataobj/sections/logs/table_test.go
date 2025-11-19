package logs

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
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

func Test_table_backfillMetadata(t *testing.T) {
	records := []Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0), Line: []byte("msg1"), Metadata: labels.FromStrings("env", "prod", "service", "api")},
		{StreamID: 2, Timestamp: time.Unix(2, 0), Line: []byte("msg2"), Metadata: labels.FromStrings("env", "prod", "service", "api", "version", "v1")},
		{StreamID: 3, Timestamp: time.Unix(3, 0), Line: []byte("msg3"), Metadata: labels.FromStrings("env", "prod")}, // Missing service and version
		{StreamID: 4, Timestamp: time.Unix(4, 0), Line: []byte("msg4"), Metadata: labels.FromStrings("env", "dev")},  // Missing service and version
	}
	table := buildTable(&tableBuffer{}, pageSize, pageRows, dataset.CompressionOptions{}, records, SortTimestampDESC)

	// All metadata columns should have the same row count due to backfill
	expectedRows := len(records)
	for _, metadata := range table.Metadatas {
		require.Equal(t, expectedRows, metadata.Desc.RowsCount, "Metadata column %s should have %d rows after backfill, got %d", metadata.Desc.Tag, expectedRows, metadata.Desc.RowsCount)
	}

	columns, err := result.Collect(table.ListColumns(context.Background()))
	require.NoError(t, err)

	r := dataset.NewReader(dataset.ReaderOptions{
		Dataset: table,
		Columns: columns,
	})

	rows := make([]dataset.Row, expectedRows)
	n, err := r.Read(context.Background(), rows)
	require.NoError(t, err)
	require.Equal(t, expectedRows, n)

	expected := []dataset.Row{
		{
			Index: 0,
			Values: []dataset.Value{
				dataset.Int64Value(4),
				dataset.Int64Value(4e9),
				dataset.BinaryValue([]byte("dev")),
				{},
				{},
				dataset.BinaryValue([]byte("msg4")),
			},
		},
		{
			Index: 1,
			Values: []dataset.Value{
				dataset.Int64Value(3),
				dataset.Int64Value(3e9),
				dataset.BinaryValue([]byte("prod")),
				{},
				{},
				dataset.BinaryValue([]byte("msg3")),
			},
		},
		{
			Index: 2,
			Values: []dataset.Value{
				dataset.Int64Value(2),
				dataset.Int64Value(2e9),
				dataset.BinaryValue([]byte("prod")),
				dataset.BinaryValue([]byte("api")),
				dataset.BinaryValue([]byte("v1")),
				dataset.BinaryValue([]byte("msg2")),
			},
		},
		{
			Index: 3,
			Values: []dataset.Value{
				dataset.Int64Value(1),
				dataset.Int64Value(1e9),
				dataset.BinaryValue([]byte("prod")),
				dataset.BinaryValue([]byte("api")),
				{},
				dataset.BinaryValue([]byte("msg1")),
			},
		},
	}

	require.Equal(t, expected, rows, "Rows should match expected data with proper backfill")
}
