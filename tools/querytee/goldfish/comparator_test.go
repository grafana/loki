package goldfish

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/stretchr/testify/assert"
)

func TestComparator_CompareResponses(t *testing.T) {
	testTolerance := 0.1 // 10% tolerance for tests
	tests := []struct {
		name           string
		sample         *goldfish.QuerySample
		expectedStatus goldfish.ComparisonStatus
		expectedDiffs  []string
	}{
		{
			name: "different status codes",
			sample: &goldfish.QuerySample{
				CorrelationID:   "test-1",
				CellAStatusCode: 200,
				CellBStatusCode: 500,
			},
			expectedStatus: goldfish.ComparisonStatusMismatch,
			expectedDiffs:  []string{"status_code"},
		},
		{
			name: "both non-200 status codes match",
			sample: &goldfish.QuerySample{
				CorrelationID:   "test-2",
				CellAStatusCode: 404,
				CellBStatusCode: 404,
			},
			expectedStatus: goldfish.ComparisonStatusMatch,
		},
		{
			name: "matching responses with same hash",
			sample: &goldfish.QuerySample{
				CorrelationID:     "test-3",
				QueryType:         "query_range",
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseHash: "abc123",
				CellBResponseHash: "abc123",
				CellAStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000},
				CellBStats:        goldfish.QueryStats{ExecTimeMs: 105, BytesProcessed: 1000},
			},
			expectedStatus: goldfish.ComparisonStatusMatch,
		},
		{
			name: "different response hashes",
			sample: &goldfish.QuerySample{
				CorrelationID:     "test-4",
				QueryType:         "query_range",
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456",
				CellAStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000},
				CellBStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000},
			},
			expectedStatus: goldfish.ComparisonStatusMismatch,
			expectedDiffs:  []string{"content_hash"},
		},
		{
			name: "different content (different hash)",
			sample: &goldfish.QuerySample{
				CorrelationID:     "test-5",
				QueryType:         "query_range",
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456", // Different hash = different content
				CellAStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
				CellBStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 2000, LinesProcessed: 50}, // Different bytes processed makes sense with different content
			},
			expectedStatus: goldfish.ComparisonStatusMismatch,
			expectedDiffs:  []string{"content_hash"},
		},
		{
			name: "execution time variance within tolerance",
			sample: &goldfish.QuerySample{
				CorrelationID:     "test-6",
				QueryType:         "query_range",
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseHash: "abc123",
				CellBResponseHash: "abc123",
				CellAStats:        goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
				CellBStats:        goldfish.QueryStats{ExecTimeMs: 105, BytesProcessed: 1000, LinesProcessed: 50}, // 5% difference, within tolerance
			},
			expectedStatus: goldfish.ComparisonStatusMatch,
		},
	}

	// Note: No comparator setup needed for simplified hash-based comparison

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareResponses(tt.sample, testTolerance)
			assert.Equal(t, tt.expectedStatus, result.ComparisonStatus)

			for _, expectedDiff := range tt.expectedDiffs {
				assert.Contains(t, result.DifferenceDetails, expectedDiff)
			}
		})
	}
}

func TestCompareQueryStats(t *testing.T) {
	tests := []struct {
		name        string
		statsA      goldfish.QueryStats
		statsB      goldfish.QueryStats
		expectMatch bool
		expectDiffs []string
	}{
		{
			name:        "identical stats",
			statsA:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
			statsB:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
			expectMatch: true,
		},
		{
			name:        "execution time within tolerance",
			statsA:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
			statsB:      goldfish.QueryStats{ExecTimeMs: 105, BytesProcessed: 1000, LinesProcessed: 50}, // 5% difference
			expectMatch: true,
		},
		{
			name:        "execution time outside tolerance",
			statsA:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
			statsB:      goldfish.QueryStats{ExecTimeMs: 120, BytesProcessed: 1000, LinesProcessed: 50}, // 20% difference
			expectMatch: false,
			expectDiffs: []string{"exec_time_variance"},
		},
		{
			name:        "different bytes processed",
			statsA:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 1000, LinesProcessed: 50},
			statsB:      goldfish.QueryStats{ExecTimeMs: 100, BytesProcessed: 2000, LinesProcessed: 50},
			expectMatch: false,
			expectDiffs: []string{"bytes_processed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &goldfish.ComparisonResult{DifferenceDetails: make(map[string]any)}
			testTolerance := 0.1 // 10% tolerance for tests
			compareQueryStats(tt.statsA, tt.statsB, result, testTolerance)

			// Check if differences were recorded when expected
			if tt.expectMatch {
				// For matches, we should have no differences (or only execution time variance which is informational)
				if len(tt.expectDiffs) == 0 {
					// Should be no significant differences
					assert.NotContains(t, result.DifferenceDetails, "bytes_processed")
					assert.NotContains(t, result.DifferenceDetails, "lines_processed")
					assert.NotContains(t, result.DifferenceDetails, "entries_returned")
				}
			} else {
				// For non-matches, check that expected differences are recorded
				for _, expectedDiff := range tt.expectDiffs {
					assert.Contains(t, result.DifferenceDetails, expectedDiff)
				}
			}
		})
	}
}

func TestCompareResponses_StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		cellAStatus    int
		cellBStatus    int
		expectedStatus goldfish.ComparisonStatus
	}{
		{
			name:           "both success",
			cellAStatus:    200,
			cellBStatus:    200,
			expectedStatus: goldfish.ComparisonStatusMatch, // Will depend on hash comparison
		},
		{
			name:           "different status codes",
			cellAStatus:    200,
			cellBStatus:    500,
			expectedStatus: goldfish.ComparisonStatusMismatch,
		},
		{
			name:           "both errors",
			cellAStatus:    404,
			cellBStatus:    404,
			expectedStatus: goldfish.ComparisonStatusMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample := &goldfish.QuerySample{
				CorrelationID:     "test",
				CellAStatusCode:   tt.cellAStatus,
				CellBStatusCode:   tt.cellBStatus,
				CellAResponseHash: "hash1",
				CellBResponseHash: "hash1", // Same hash for successful cases
			}

			testTolerance := 0.1 // 10% tolerance for tests
			result := CompareResponses(sample, testTolerance)

			if tt.cellAStatus == 200 && tt.cellBStatus == 200 {
				// For 200 status codes, result depends on hash comparison
				assert.Equal(t, goldfish.ComparisonStatusMatch, result.ComparisonStatus)
			} else {
				assert.Equal(t, tt.expectedStatus, result.ComparisonStatus)
			}
		})
	}
}

func TestCompareResponses_ConfigurableTolerance(t *testing.T) {
	tests := []struct {
		name           string
		tolerance      float64
		cellAExecTime  int64
		cellBExecTime  int64
		expectVariance bool
	}{
		{
			name:           "5% difference with 10% tolerance - no variance",
			tolerance:      0.1,
			cellAExecTime:  100,
			cellBExecTime:  105,
			expectVariance: false,
		},
		{
			name:           "15% difference with 10% tolerance - has variance",
			tolerance:      0.1,
			cellAExecTime:  100,
			cellBExecTime:  115,
			expectVariance: true,
		},
		{
			name:           "5% difference with 1% tolerance - has variance",
			tolerance:      0.01,
			cellAExecTime:  100,
			cellBExecTime:  105,
			expectVariance: true,
		},
		{
			name:           "50% difference with 60% tolerance - no variance",
			tolerance:      0.6,
			cellAExecTime:  100,
			cellBExecTime:  150,
			expectVariance: false,
		},
		{
			name:           "0% tolerance requires exact match",
			tolerance:      0.0,
			cellAExecTime:  100,
			cellBExecTime:  101,
			expectVariance: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample := &goldfish.QuerySample{
				CorrelationID:     "test-tolerance",
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseHash: "same-hash",
				CellBResponseHash: "same-hash",
				CellAStats: goldfish.QueryStats{
					ExecTimeMs:     tt.cellAExecTime,
					BytesProcessed: 1000,
				},
				CellBStats: goldfish.QueryStats{
					ExecTimeMs:     tt.cellBExecTime,
					BytesProcessed: 1000,
				},
			}

			result := CompareResponses(sample, tt.tolerance)

			// The comparison should always be a match (same hash)
			assert.Equal(t, goldfish.ComparisonStatusMatch, result.ComparisonStatus)

			// Check if execution time variance was detected based on tolerance
			if tt.expectVariance {
				assert.Contains(t, result.DifferenceDetails, "exec_time_variance")
			} else {
				assert.NotContains(t, result.DifferenceDetails, "exec_time_variance")
			}
		})
	}
}
