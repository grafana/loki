package goldfish

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsExtractor_NewEngineWarningDetection(t *testing.T) {
	extractor := NewStatsExtractor()

	tests := []struct {
		name               string
		responseBody       string
		expectedUsed       bool
		expectedResultType string
		expectedError      bool
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
			expectedUsed:       true,
			expectedResultType: "streams",
			expectedError:      false,
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
			expectedUsed:       false,
			expectedResultType: "streams",
			expectedError:      false,
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
			expectedUsed:       false,
			expectedResultType: "streams",
			expectedError:      false,
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
			expectedUsed:       true,
			expectedResultType: "streams",
			expectedError:      false,
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
			expectedUsed:       false,
			expectedResultType: "streams",
			expectedError:      false,
		},
		{
			name:               "invalid JSON",
			responseBody:       `{invalid json}`,
			expectedUsed:       false,
			expectedResultType: "",
			expectedError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, hash, size, usedNewEngine, resultType, err := extractor.ExtractResponseData([]byte(tt.responseBody), 50)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedUsed, usedNewEngine, "usedNewEngine mismatch")
				assert.Equal(t, tt.expectedResultType, string(resultType), "resultType mismatch")

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

func TestStatsExtractor_EntriesCountedFromResponseData(t *testing.T) {
	extractor := NewStatsExtractor()

	tests := []struct {
		name            string
		responseBody    string
		expectedEntries int64
	}{
		{
			name: "stream entries counted from result, not stats",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [
						{
							"stream": {"job": "test"},
							"values": [
								["1700000000000000000", "line 1"],
								["1700000000001000000", "line 2"],
								["1700000000002000000", "line 3"]
							]
						}
					],
					"stats": {
						"summary": {
							"totalEntriesReturned": 999
						}
					}
				}
			}`,
			expectedEntries: 3,
		},
		{
			name: "multiple streams entries summed",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [
						{
							"stream": {"job": "a"},
							"values": [["1700000000000000000", "line 1"], ["1700000000001000000", "line 2"]]
						},
						{
							"stream": {"job": "b"},
							"values": [["1700000000000000000", "line 3"]]
						}
					],
					"stats": {
						"summary": {
							"totalEntriesReturned": 50
						}
					}
				}
			}`,
			expectedEntries: 3,
		},
		{
			name: "empty streams returns zero",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [],
					"stats": {
						"summary": {
							"totalEntriesReturned": 10
						}
					}
				}
			}`,
			expectedEntries: 0,
		},
		{
			name: "same result data with different stats produces same entry count",
			responseBody: `{
				"status": "success",
				"data": {
					"resultType": "streams",
					"result": [
						{
							"stream": {"job": "test"},
							"values": [["1700000000000000000", "line 1"], ["1700000000001000000", "line 2"]]
						}
					],
					"stats": {
						"summary": {
							"totalEntriesReturned": 200,
							"splits": 5,
							"shards": 10
						}
					}
				}
			}`,
			expectedEntries: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, _, _, _, _, err := extractor.ExtractResponseData([]byte(tt.responseBody), 50)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedEntries, stats.TotalEntriesReturned,
				"TotalEntriesReturned should be counted from actual response entries, not stats")
		})
	}
}

func TestStatsExtractor_IdenticalResultsDifferentStats(t *testing.T) {
	extractor := NewStatsExtractor()

	// Two responses with identical result data but different stats (simulating v1 vs v2 cells)
	cellAResponse := `{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [
				{
					"stream": {"job": "test", "env": "prod"},
					"values": [
						["1700000000000000000", "request completed"],
						["1700000000001000000", "request started"]
					]
				}
			],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"totalBytesProcessed": 1000,
					"totalEntriesReturned": 2,
					"splits": 1,
					"shards": 4
				}
			}
		}
	}`

	cellBResponse := `{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [
				{
					"stream": {"job": "test", "env": "prod"},
					"values": [
						["1700000000000000000", "request completed"],
						["1700000000001000000", "request started"]
					]
				}
			],
			"stats": {
				"summary": {
					"execTime": 0.08,
					"totalBytesProcessed": 2000,
					"totalEntriesReturned": 150,
					"splits": 3,
					"shards": 8
				}
			}
		}
	}`

	statsA, hashA, _, _, _, err := extractor.ExtractResponseData([]byte(cellAResponse), 50)
	require.NoError(t, err)

	statsB, hashB, _, _, _, err := extractor.ExtractResponseData([]byte(cellBResponse), 80)
	require.NoError(t, err)

	assert.Equal(t, hashA, hashB, "identical result data should produce the same hash")
	assert.Equal(t, statsA.TotalEntriesReturned, statsB.TotalEntriesReturned,
		"identical result data should produce the same entry count regardless of stats differences")
	assert.Equal(t, int64(2), statsA.TotalEntriesReturned,
		"entry count should reflect the 2 actual entries in the response")
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
