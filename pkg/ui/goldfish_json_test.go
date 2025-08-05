package ui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestSampledQueryJSONMarshaling(t *testing.T) {
	// Create a test QuerySample
	now := time.Now()
	qs := goldfish.QuerySample{
		CorrelationID: "test-123",
		TenantID:      "tenant-1",
		Query:         "{job=\"test\"}",
		QueryType:     "range",
		StartTime:     now.Add(-1 * time.Hour),
		EndTime:       now,
		Step:          5 * time.Minute,
		CellAStats: goldfish.QueryStats{
			ExecTimeMs:           100,
			QueueTimeMs:          10,
			BytesProcessed:       1000,
			LinesProcessed:       50,
			BytesPerSecond:       10000,
			LinesPerSecond:       500,
			TotalEntriesReturned: 25,
			Splits:               2,
			Shards:               4,
		},
		CellBStats: goldfish.QueryStats{
			ExecTimeMs:           110,
			QueueTimeMs:          12,
			BytesProcessed:       1100,
			LinesProcessed:       55,
			BytesPerSecond:       10000,
			LinesPerSecond:       500,
			TotalEntriesReturned: 25,
			Splits:               2,
			Shards:               4,
		},
		CellAResponseHash: "hash-a",
		CellBResponseHash: "hash-b",
		CellAResponseSize: 2000,
		CellBResponseSize: 2100,
		CellAStatusCode:   200,
		CellBStatusCode:   200,
		CellATraceID:      "trace-a",
		CellBTraceID:      "trace-b",
		SampledAt:         now,
	}

	// Create SampledQuery from QuerySample data
	sq := SampledQuery{
		// Core fields
		CorrelationID: qs.CorrelationID,
		TenantID:      qs.TenantID,
		Query:         qs.Query,
		QueryType:     qs.QueryType,

		// Time fields - convert to RFC3339 strings
		StartTime:    qs.StartTime.Format(time.RFC3339),
		EndTime:      qs.EndTime.Format(time.RFC3339),
		StepDuration: int64Ptr(qs.Step.Milliseconds()),

		// Timestamps
		SampledAt: qs.SampledAt,
		CreatedAt: qs.SampledAt,

		// Flattened performance stats
		CellAExecTimeMs:      &qs.CellAStats.ExecTimeMs,
		CellBExecTimeMs:      &qs.CellBStats.ExecTimeMs,
		CellAQueueTimeMs:     &qs.CellAStats.QueueTimeMs,
		CellBQueueTimeMs:     &qs.CellBStats.QueueTimeMs,
		CellABytesProcessed:  &qs.CellAStats.BytesProcessed,
		CellBBytesProcessed:  &qs.CellBStats.BytesProcessed,
		CellALinesProcessed:  &qs.CellAStats.LinesProcessed,
		CellBLinesProcessed:  &qs.CellBStats.LinesProcessed,
		CellABytesPerSecond:  &qs.CellAStats.BytesPerSecond,
		CellBBytesPerSecond:  &qs.CellBStats.BytesPerSecond,
		CellALinesPerSecond:  &qs.CellAStats.LinesPerSecond,
		CellBLinesPerSecond:  &qs.CellBStats.LinesPerSecond,
		CellAEntriesReturned: &qs.CellAStats.TotalEntriesReturned,
		CellBEntriesReturned: &qs.CellBStats.TotalEntriesReturned,
		CellASplits:          &qs.CellAStats.Splits,
		CellBSplits:          &qs.CellBStats.Splits,
		CellAShards:          &qs.CellAStats.Shards,
		CellBShards:          &qs.CellBStats.Shards,

		// Response metadata as pointers
		CellAResponseHash: strPtr(qs.CellAResponseHash),
		CellBResponseHash: strPtr(qs.CellBResponseHash),
		CellAResponseSize: &qs.CellAResponseSize,
		CellBResponseSize: &qs.CellBResponseSize,
		CellAStatusCode:   intPtr(qs.CellAStatusCode),
		CellBStatusCode:   intPtr(qs.CellBStatusCode),
		CellATraceID:      strPtr(qs.CellATraceID),
		CellBTraceID:      strPtr(qs.CellBTraceID),

		// UI-only fields
		CellATraceLink:   strPtr("https://grafana.com/explore?trace-a"),
		CellBTraceLink:   strPtr("https://grafana.com/explore?trace-b"),
		ComparisonStatus: "mismatch",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(sq)
	require.NoError(t, err)

	// Parse the JSON to verify structure
	var result map[string]interface{}
	err = json.Unmarshal(jsonData, &result)
	require.NoError(t, err)

	// Verify key fields are at the top level
	require.Equal(t, "test-123", result["correlationId"])
	require.Equal(t, "tenant-1", result["tenantId"])
	require.Equal(t, "{job=\"test\"}", result["query"])
	require.Equal(t, "range", result["queryType"])
	require.Equal(t, "mismatch", result["comparisonStatus"])

	// Verify performance stats are flattened
	require.Equal(t, float64(100), result["cellAExecTimeMs"])
	require.Equal(t, float64(110), result["cellBExecTimeMs"])
	require.Equal(t, float64(1000), result["cellABytesProcessed"])
	require.Equal(t, float64(1100), result["cellBBytesProcessed"])

	// Verify trace links are included
	require.Equal(t, "https://grafana.com/explore?trace-a", result["cellATraceLink"])
	require.Equal(t, "https://grafana.com/explore?trace-b", result["cellBTraceLink"])

	// Verify there are no nested objects for stats
	require.NotContains(t, result, "cellAStats")
	require.NotContains(t, result, "cellBStats")

	// Print JSON for manual inspection
	t.Logf("JSON output:\n%s", string(jsonData))
}
