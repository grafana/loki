package main

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

var testFacts = []factRow{
	{
		Tenant:           "tenant-a",
		IndexObject:      "indexes/00/abc123",
		IndexSection:     3,
		ColumnName:       "service_name",
		LabelValue:       "frontend",
		LogsObject:       "logs/00/def456",
		LogsSection:      2,
		StreamRefs:       5,
		UncompressedSize: 1024,
	},
	{
		Tenant:           "tenant-a",
		IndexObject:      "indexes/tenants/tenant-a/00/ghi789",
		IndexSection:     5,
		Compacted:        true,
		ColumnName:       "service_name",
		LabelValue:       "backend",
		LogsObject:       "logs/00/ghi789",
		LogsSection:      0,
		StreamRefs:       3,
		UncompressedSize: 512,
	},
}

func TestParquetFactSink_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.parquet")
	ps, err := newParquetFactSink(path)
	require.NoError(t, err)

	require.NoError(t, ps.write(testFacts))
	require.NoError(t, ps.close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	reader := parquet.NewGenericReader[factRow](f)
	defer reader.Close()

	got := make([]factRow, len(testFacts))
	n, err := reader.Read(got)
	// parquet-go returns io.EOF together with the last batch of rows.
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	require.Equal(t, len(testFacts), n)

	require.Equal(t, testFacts[0].Tenant, got[0].Tenant)
	require.Equal(t, testFacts[0].IndexSection, got[0].IndexSection)
	require.Equal(t, testFacts[0].ColumnName, got[0].ColumnName)
	require.Equal(t, testFacts[0].LabelValue, got[0].LabelValue)
	require.Equal(t, testFacts[0].LogsSection, got[0].LogsSection)
	require.Equal(t, testFacts[0].StreamRefs, got[0].StreamRefs)
	require.Equal(t, testFacts[0].UncompressedSize, got[0].UncompressedSize)
	require.Equal(t, testFacts[0].Compacted, got[0].Compacted)

	require.Equal(t, testFacts[1].LabelValue, got[1].LabelValue)
	require.Equal(t, testFacts[1].IndexSection, got[1].IndexSection)
	require.Equal(t, testFacts[1].Compacted, got[1].Compacted)
}

func TestCSVFactSink_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.csv")
	cs, err := newCSVFactSink(path)
	require.NoError(t, err)

	require.NoError(t, cs.write(testFacts))
	require.NoError(t, cs.close())

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	require.NoError(t, err)

	// header + 2 data rows
	require.Len(t, records, 3)
	require.Equal(t, csvHeader, records[0])

	// Spot-check data row 0.
	// Column layout: tenant(0) index_object(1) index_section(2) compacted(3)
	//                column_name(4) label_value(5) logs_object(6) logs_section(7)
	//                stream_refs(8) uncompressed_size(9)
	require.Equal(t, "tenant-a", records[1][0])
	require.Equal(t, "3", records[1][2])     // index_section
	require.Equal(t, "false", records[1][3]) // compacted
	require.Equal(t, "service_name", records[1][4])
	require.Equal(t, "frontend", records[1][5])
	require.Equal(t, "2", records[1][7])    // logs_section
	require.Equal(t, "5", records[1][8])    // stream_refs
	require.Equal(t, "1024", records[1][9]) // uncompressed_size

	// Spot-check data row 1.
	require.Equal(t, "true", records[2][3]) // compacted
	require.Equal(t, "backend", records[2][5])
	require.Equal(t, "5", records[2][2]) // index_section
}

func TestNewFactSink_Both(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "facts")

	sink, err := newFactSink(prefix, "both")
	require.NoError(t, err)

	require.NoError(t, sink.write(testFacts))
	require.NoError(t, sink.close())

	_, err = os.Stat(prefix + ".parquet")
	require.NoError(t, err, "parquet file should exist")

	_, err = os.Stat(prefix + ".csv")
	require.NoError(t, err, "csv file should exist")
}

func TestNewFactSink_ParquetOnly(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "facts")

	sink, err := newFactSink(prefix, "parquet")
	require.NoError(t, err)
	require.NoError(t, sink.write(testFacts))
	require.NoError(t, sink.close())

	_, err = os.Stat(prefix + ".parquet")
	require.NoError(t, err)

	_, err = os.Stat(prefix + ".csv")
	require.ErrorIs(t, err, os.ErrNotExist, "csv file should not exist for parquet-only format")
}

func TestNewFactSink_CSVOnly(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "facts")

	sink, err := newFactSink(prefix, "csv")
	require.NoError(t, err)
	require.NoError(t, sink.write(testFacts))
	require.NoError(t, sink.close())

	_, err = os.Stat(prefix + ".csv")
	require.NoError(t, err)

	_, err = os.Stat(prefix + ".parquet")
	require.ErrorIs(t, err, os.ErrNotExist, "parquet file should not exist for csv-only format")
}
