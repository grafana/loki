package goldfish

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsExtractor_NewEngineWarningDetection(t *testing.T) {
	extractor := NewStatsExtractor()

	tests := []struct {
		name          string
		responseBody  string
		expectedUsed  bool
		expectedError bool
	}{
		{
			name: "response with new engine warning",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [{
						"stream": {"job": "test"},
						"values": [["1700000000000000000", "log line"]]
					}],
					"stats": {
						"summary": {
							"execTime": 0.05,
							"queueTime": 0.01,
							"totalBytesProcessed": 1000,
							"totalLinesProcessed": 10,
							"bytesProcessedPerSecond": 20000,
							"linesProcessedPerSecond": 200,
							"totalEntriesReturned": 1,
							"splits": 1,
							"shards": 1
						}
					}
				},
				"warnings": [
					"Query was executed using the new experimental query engine and dataobj storage."
				]
			}`,
			expectedUsed:  true,
			expectedError: false,
		},
		{
			name: "response without warnings",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [{
						"stream": {"job": "test"},
						"values": [["1700000000000000000", "log line"]]
					}],
					"stats": {
						"summary": {
							"execTime": 0.05,
							"queueTime": 0.01,
							"totalBytesProcessed": 1000,
							"totalLinesProcessed": 10,
							"bytesProcessedPerSecond": 20000,
							"linesProcessedPerSecond": 200,
							"totalEntriesReturned": 1,
							"splits": 1,
							"shards": 1
						}
					}
				}
			}`,
			expectedUsed:  false,
			expectedError: false,
		},
		{
			name: "response with different warning",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [{
						"stream": {"job": "test"},
						"values": [["1700000000000000000", "log line"]]
					}],
					"stats": {
						"summary": {
							"execTime": 0.05,
							"queueTime": 0.01,
							"totalBytesProcessed": 1000,
							"totalLinesProcessed": 10,
							"bytesProcessedPerSecond": 20000,
							"linesProcessedPerSecond": 200,
							"totalEntriesReturned": 1,
							"splits": 1,
							"shards": 1
						}
					}
				},
				"warnings": [
					"Some other warning message",
					"Query processing took longer than expected"
				]
			}`,
			expectedUsed:  false,
			expectedError: false,
		},
		{
			name: "response with new engine warning among others",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [{
						"stream": {"job": "test"},
						"values": [["1700000000000000000", "log line"]]
					}],
					"stats": {
						"summary": {
							"execTime": 0.05,
							"queueTime": 0.01,
							"totalBytesProcessed": 1000,
							"totalLinesProcessed": 10,
							"bytesProcessedPerSecond": 20000,
							"linesProcessedPerSecond": 200,
							"totalEntriesReturned": 1,
							"splits": 1,
							"shards": 1
						}
					}
				},
				"warnings": [
					"Query processing took longer than expected",
					"Query was executed using the new experimental query engine and dataobj storage.",
					"Large dataset detected"
				]
			}`,
			expectedUsed:  true,
			expectedError: false,
		},
		{
			name: "empty warnings array",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [{
						"stream": {"job": "test"},
						"values": [["1700000000000000000", "log line"]]
					}],
					"stats": {
						"summary": {
							"execTime": 0.05,
							"queueTime": 0.01,
							"totalBytesProcessed": 1000,
							"totalLinesProcessed": 10,
							"bytesProcessedPerSecond": 20000,
							"linesProcessedPerSecond": 200,
							"totalEntriesReturned": 1,
							"splits": 1,
							"shards": 1
						}
					}
				},
				"warnings": []
			}`,
			expectedUsed:  false,
			expectedError: false,
		},
		{
			name: "invalid JSON",
			responseBody: `{
				"status": "success",
				"data": {
					invalid json
				}
			}`,
			expectedUsed:  false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, hash, size, usedNewEngine, err := extractor.ExtractResponseData([]byte(tt.responseBody), 50)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedUsed, usedNewEngine, "usedNewEngine mismatch")

				// Verify other fields are still extracted correctly
				if stats.ExecTimeMs > 0 {
					assert.Equal(t, int64(50), stats.ExecTimeMs) // Should use duration-based time
					assert.Equal(t, int64(10), stats.QueueTimeMs)
					assert.Equal(t, int64(1000), stats.BytesProcessed)
					assert.Equal(t, int64(10), stats.LinesProcessed)
				}
				assert.NotEmpty(t, hash)
				assert.Greater(t, size, int64(0))
			}
		})
	}
}

func TestStatsExtractor_CheckForNewEngineWarning(t *testing.T) {
	extractor := NewStatsExtractor()

	tests := []struct {
		name     string
		warnings []string
		expected bool
	}{
		{
			name:     "exact match",
			warnings: []string{"Query was executed using the new experimental query engine and dataobj storage."},
			expected: true,
		},
		{
			name:     "no match",
			warnings: []string{"Some other warning"},
			expected: false,
		},
		{
			name:     "empty warnings",
			warnings: []string{},
			expected: false,
		},
		{
			name:     "nil warnings",
			warnings: nil,
			expected: false,
		},
		{
			name: "partial match should not count",
			warnings: []string{
				"Query was executed using the new engine",
				"experimental query engine and dataobj storage",
			},
			expected: false,
		},
		{
			name: "multiple warnings with match",
			warnings: []string{
				"Warning 1",
				"Query was executed using the new experimental query engine and dataobj storage.",
				"Warning 3",
			},
			expected: true,
		},
		{
			name: "case sensitive check",
			warnings: []string{
				"query was executed using the new experimental query engine and dataobj storage.",
			},
			expected: false, // Should be case sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.checkForNewEngineWarning(tt.warnings)
			assert.Equal(t, tt.expected, result)
		})
	}
}