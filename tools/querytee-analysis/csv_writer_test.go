package main

import (
	"encoding/csv"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTime(t *testing.T) {
	t.Run("zero time", func(t *testing.T) {
		assert.Equal(t, "", formatTime(time.Time{}))
	})

	t.Run("valid time", func(t *testing.T) {
		ts := time.Date(2025, 3, 4, 12, 30, 0, 0, time.UTC)
		assert.Equal(t, "2025-03-04T12:30:00Z", formatTime(ts))
	})
}

func TestResultToRow(t *testing.T) {
	entry := &MismatchEntry{
		CorrelationID:        "corr-1",
		Tenant:               "tenant-1",
		Query:                `{app="foo"}`,
		QueryType:            "range",
		StartTime:            time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:              time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC),
		MismatchCause:        "stream_entry_count",
		CellAEntriesReturned: 100,
		CellBEntriesReturned: 95,
		CellAStatusCode:      200,
		CellBStatusCode:      200,
	}

	result := &AnalysisResult{
		MismatchEntry:                 entry,
		Phase:                         3,
		PredictedCause:                PredictedCauseDataMismatch,
		Phase1ComparisonMismatchCause: "value_sample_pair_mismatch",
		Phase2CountQuery:              `sum(count_over_time({app="foo"} [3600s]))`,
		Phase2CellACount:              100,
		Phase2CellBCount:              95,
		DetectedMismatchType:          "stream_entry_count",
		MismatchedItem:                `{app="foo"}`,
		Details:                       "cellA has 100, cellB has 95",
	}

	row := resultToRow(result)
	require.Len(t, row, len(csvHeaders), "row length must match header count")

	assert.Equal(t, "corr-1", row[0])
	assert.Equal(t, "tenant-1", row[1])
	assert.Equal(t, `{app="foo"}`, row[2])
	assert.Equal(t, "range", row[3])
	assert.Contains(t, row[4], "2025-01-01")
	assert.Equal(t, "stream_entry_count", row[6])
	assert.Equal(t, "3", row[7])
	assert.Equal(t, "data_mismatch", row[8])
	assert.Equal(t, "value_sample_pair_mismatch", row[9])
	assert.Equal(t, `sum(count_over_time({app="foo"} [3600s]))`, row[10])
	assert.Equal(t, "100", row[11])
	assert.Equal(t, "95", row[12])
	assert.Equal(t, "stream_entry_count", row[13])
	assert.Equal(t, `{app="foo"}`, row[14])
	assert.Equal(t, "cellA has 100, cellB has 95", row[15])
}

func TestResultToRow_Phase1(t *testing.T) {
	entry := &MismatchEntry{CorrelationID: "x"}
	result := &AnalysisResult{
		MismatchEntry:  entry,
		Phase:          1,
		PredictedCause: PredictedCauseIngestionDelay,
	}

	row := resultToRow(result)
	require.Len(t, row, len(csvHeaders))

	assert.Equal(t, "1", row[7])
	assert.Equal(t, "ingestion_pipeline_delay", row[8])
	assert.Equal(t, "", row[9])  // no phase1 mismatch cause (it matched)
	assert.Equal(t, "", row[10]) // no count query
	assert.Equal(t, "0", row[11])
	assert.Equal(t, "0", row[12])
	assert.Equal(t, "", row[13]) // no detected_mismatch_type (Phase 3 only)
}

func TestResultToRow_Phase2_Match(t *testing.T) {
	entry := &MismatchEntry{CorrelationID: "x"}
	result := &AnalysisResult{
		MismatchEntry:    entry,
		Phase:            2,
		PredictedCause:   PredictedCauseQueryExecBug,
		Phase2CountQuery: `sum(count_over_time({app="foo"} [3600s]))`,
		Phase2CellACount: 500,
		Phase2CellBCount: 500,
		Details:          "all 1 series match",
	}

	row := resultToRow(result)
	require.Len(t, row, len(csvHeaders))

	assert.Equal(t, "2", row[7])
	assert.Equal(t, "query_execution_bug", row[8])
	assert.Equal(t, `sum(count_over_time({app="foo"} [3600s]))`, row[10])
	assert.Equal(t, "500", row[11])
	assert.Equal(t, "500", row[12])
	assert.Contains(t, row[15], "all 1 series match")
}

func TestResultToRow_Phase2_Mismatch(t *testing.T) {
	entry := &MismatchEntry{CorrelationID: "x"}
	result := &AnalysisResult{
		MismatchEntry:    entry,
		Phase:            2,
		PredictedCause:   PredictedCauseLogCountMismatch,
		Phase2CountQuery: `sum by (level)(count_over_time({app="foo"} | logfmt [3600s]))`,
		Phase2CellACount: 500,
		Phase2CellBCount: 480,
		Details:          "4 series total: 2 match, 1 count_mismatch, 0 missing_in_cell_a, 1 missing_in_cell_b",
	}

	row := resultToRow(result)
	require.Len(t, row, len(csvHeaders))

	assert.Equal(t, "2", row[7])
	assert.Equal(t, "log_count_mismatch", row[8])
	assert.Equal(t, "500", row[11])
	assert.Equal(t, "480", row[12])
	assert.Contains(t, row[15], "4 series total")
}

func TestWriteCSV(t *testing.T) {
	tmpFile := t.TempDir() + "/test_output.csv"

	entry := &MismatchEntry{
		CorrelationID: "c1",
		Tenant:        "t1",
		Query:         `{app="foo"}`,
	}

	results := []*AnalysisResult{
		{
			MismatchEntry:  entry,
			Phase:          1,
			PredictedCause: PredictedCauseIngestionDelay,
		},
		nil,
		{
			MismatchEntry:  entry,
			Phase:          2,
			PredictedCause: PredictedCauseLogCountMismatch,
		},
	}

	err := writeCSV(tmpFile, results)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	require.NoError(t, err)

	assert.Len(t, records, 3, "header + 2 data rows (nil skipped)")
	assert.Equal(t, csvHeaders, records[0])
	assert.Equal(t, "c1", records[1][0])
	assert.Equal(t, "1", records[1][7])
	assert.Equal(t, "2", records[2][7])
}

func TestWriteCSV_EmptyResults(t *testing.T) {
	tmpFile := t.TempDir() + "/empty.csv"
	err := writeCSV(tmpFile, nil)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	require.NoError(t, err)
	assert.Len(t, records, 1, "only header row")
}

func TestWriteCSV_SkipsNilEntry(t *testing.T) {
	tmpFile := t.TempDir() + "/nil_entry.csv"
	results := []*AnalysisResult{
		{MismatchEntry: nil},
	}
	err := writeCSV(tmpFile, results)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	require.NoError(t, err)
	assert.Len(t, records, 1, "only header, nil entry skipped")
}
