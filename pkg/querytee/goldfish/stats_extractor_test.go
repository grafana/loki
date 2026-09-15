package goldfish

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsExtractor_NewEngineDetection(t *testing.T) {
	extractor := NewStatsExtractor()

	tests := []struct {
		name               string
		responseBody       string
		expectedUsed       bool
		expectedResultType string
		expectedError      bool
	}{
		{
			name: "response with v2 engine flag in stats",
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
						},
						"querier": {
							"store": {
								"queryUsedV2Engine": true
							}
						}
					}
				}
			}`,
			expectedUsed:       true,
			expectedResultType: "streams",
		},
		{
			name: "warning text is ignored",
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
						},
						"querier": {
							"store": {
								"queryUsedV2Engine": false
							}
						}
					}
				},
				"warnings": [
					"Query was executed using the next-generation Loki query engine."
				]
			}`,
			expectedUsed:       false,
			expectedResultType: "streams",
		},
		{
			name: "missing engine flag defaults to false",
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
		},
		{
			name:               "invalid JSON",
			responseBody:       `{invalid json}`,
			expectedResultType: "",
			expectedError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, hash, size, usedNewEngine, resultType, err := extractor.ExtractResponseData([]byte(tt.responseBody), 50)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedUsed, usedNewEngine, "usedNewEngine mismatch")
			assert.Equal(t, tt.expectedResultType, string(resultType), "resultType mismatch")
			assert.Equal(t, int64(50), stats.ExecTimeMs)
			assert.Equal(t, int64(10), stats.QueueTimeMs)
			assert.Equal(t, int64(1000), stats.BytesProcessed)
			assert.Equal(t, int64(10), stats.LinesProcessed)
			assert.NotEmpty(t, hash)
			assert.Greater(t, size, int64(0))
		})
	}
}
