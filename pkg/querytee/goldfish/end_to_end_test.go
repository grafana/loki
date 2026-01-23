package goldfish

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestGoldfishEndToEnd(t *testing.T) {
	// This test demonstrates the full flow of Goldfish functionality

	// Configure Goldfish
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0, // Sample everything for testing
			TenantRules: map[string]float64{
				"tenant1": 1.0,
				"tenant2": 0.0, // Don't sample tenant2
			},
		},
	}

	// Create mock storage
	storage := &mockStorage{}

	// Create manager
	manager, err := NewManager(config, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	defer manager.Close()

	// Test sampling decisions
	assert.True(t, manager.ShouldSample("tenant1"))
	assert.False(t, manager.ShouldSample("tenant2"))
	assert.True(t, manager.ShouldSample("tenant3")) // Uses default rate

	// Create test HTTP request
	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}&start=1700000000&end=1700001000", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	// Simulate responses from Cell A and Cell B
	responseBodyA := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [
					["1700000000000000000", "log line 1"],
					["1700000001000000000", "log line 2"]
				]
			}],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 1000,
					"totalLinesProcessed": 2,
					"bytesProcessedPerSecond": 20000,
					"linesProcessedPerSecond": 40,
					"totalEntriesReturned": 2,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	responseBodyB := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [
					["1700000000000000000", "log line 1"],
					["1700000001000000000", "log line 2"]
				]
			}],
			"stats": {
				"summary": {
					"execTime": 0.055,
					"queueTime": 0.015,
					"totalBytesProcessed": 1000,
					"totalLinesProcessed": 2,
					"bytesProcessedPerSecond": 18000,
					"linesProcessedPerSecond": 36,
					"totalEntriesReturned": 2,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        responseBodyA,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        responseBodyB,
		Duration:    55 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	// Send to Goldfish for processing
	manager.SendToGoldfish(req, cellAResp, cellBResp)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify results
	assert.Len(t, storage.samples, 1)
	assert.Len(t, storage.results, 1)

	// Check the sample
	sample := storage.samples[0]
	assert.Equal(t, "tenant1", sample.TenantID)
	assert.Equal(t, "{job=\"test\"}", sample.Query)
	assert.Equal(t, "query_range", sample.QueryType)
	assert.Equal(t, 200, sample.CellAStatusCode)
	assert.Equal(t, 200, sample.CellBStatusCode)
	assert.Equal(t, int64(50), sample.CellAStats.ExecTimeMs)
	assert.Equal(t, int64(55), sample.CellBStats.ExecTimeMs)
	assert.Equal(t, int64(1000), sample.CellAStats.BytesProcessed)
	assert.Equal(t, int64(1000), sample.CellBStats.BytesProcessed)
	// Since the response content is identical, hashes should match
	assert.Equal(t, sample.CellAResponseHash, sample.CellBResponseHash)
	assert.NotEmpty(t, sample.CellAResponseHash)
	assert.NotEmpty(t, sample.CellBResponseHash)
	// Verify engine tracking fields are populated
	assert.False(t, sample.CellAUsedNewEngine) // No new engine warning in response
	assert.False(t, sample.CellBUsedNewEngine) // No new engine warning in response

	// Check the comparison result
	result := storage.results[0]
	assert.Equal(t, goldfish.ComparisonStatusMatch, result.ComparisonStatus) // Same content = match
	assert.Equal(t, sample.CorrelationID, result.CorrelationID)
	assert.Equal(t, 50*time.Millisecond, result.PerformanceMetrics.CellAQueryTime)
	assert.Equal(t, 55*time.Millisecond, result.PerformanceMetrics.CellBQueryTime)
	assert.InDelta(t, 1.1, result.PerformanceMetrics.QueryTimeRatio, 0.01)
}

func TestGoldfishMismatchDetection(t *testing.T) {
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
	}

	storage := &mockStorage{}
	manager, err := NewManager(config, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	defer manager.Close()

	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	// Different log lines between cells - this will produce different hashes
	responseBodyA := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [["1700000000000000000", "log line A"]]
			}],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	responseBodyB := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [["1700000000000000000", "log line B"]]
			}],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        responseBodyA,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        responseBodyB,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	manager.SendToGoldfish(req, cellAResp, cellBResp)

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, storage.results, 1)
	result := storage.results[0]

	// Different hashes should result in mismatch
	assert.Equal(t, goldfish.ComparisonStatusMismatch, result.ComparisonStatus)
	assert.Contains(t, result.DifferenceDetails, "content_hash")
}

func TestGoldfishFloatingPointMismatchDetection(t *testing.T) {
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
		CompareValuesTolerance: 0.000001, // Enable tolerance comparison for tests
	}

	storage := &mockStorage{}
	manager, err := NewManager(config, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	defer manager.Close()

	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	// Different log lines between cells - this will produce different hashes
	responseBodyA := []byte(`{
		"status": "success",
		"data": {
			"resultType": "scalar",
			"result": [1, "0.003333333333"],
			"stats": {
				"summary": {
					"execTime": 0.06,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	responseBodyB := []byte(`{
		"status": "success",
		"data": {
			"resultType": "scalar",
			"result": [1, "0.0033333333335"],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        responseBodyA,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        responseBodyB,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	manager.SendToGoldfish(req, cellAResp, cellBResp)

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, storage.results, 1)
	result := storage.results[0]

	assert.Equal(t, goldfish.ComparisonStatusMismatch, result.ComparisonStatus)
	// Verify that the mismatch is due to floating point variance within tolerance
	assert.True(t, result.MatchWithinTolerance, "Result should indicate match within tolerance")

	// Verify that we recorded the compareQueryStats
	assert.Contains(t, result.DifferenceDetails, "exec_time_variance")
}

func TestGoldfishNewEngineDetection(t *testing.T) {
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
	}

	storage := &mockStorage{}
	manager, err := NewManager(config, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	defer manager.Close()

	req := httptest.NewRequest("GET", "/loki/api/v1/query_range?query={job=\"test\"}", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	// Cell A response with new engine warning
	responseBodyA := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [["1700000000000000000", "log line 1"]]
			}],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		},
		"warnings": [
			"Query was executed using the new experimental query engine and dataobj storage."
		]
	}`)

	// Cell B response without new engine warning
	responseBodyB := []byte(`{
		"status": "success",
		"data": {
			"resultType": "streams",
			"result": [{
				"stream": {"job": "test"},
				"values": [["1700000000000000000", "log line 1"]]
			}],
			"stats": {
				"summary": {
					"execTime": 0.05,
					"queueTime": 0.01,
					"totalBytesProcessed": 500,
					"totalLinesProcessed": 1,
					"bytesProcessedPerSecond": 10000,
					"linesProcessedPerSecond": 20,
					"totalEntriesReturned": 1,
					"splits": 1,
					"shards": 1
				}
			}
		}
	}`)

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        responseBodyA,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        responseBodyB,
		Duration:    50 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	manager.SendToGoldfish(req, cellAResp, cellBResp)

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, storage.samples, 1)
	sample := storage.samples[0]

	// Verify that new engine detection works correctly
	assert.True(t, sample.CellAUsedNewEngine, "Cell A should have used new engine")
	assert.False(t, sample.CellBUsedNewEngine, "Cell B should not have used new engine")
}
