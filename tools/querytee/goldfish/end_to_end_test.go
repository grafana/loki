package goldfish

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	manager, err := NewManager(config, storage, log.NewNopLogger(), prometheus.NewRegistry())
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

	// Extract stats for both responses
	extractor := NewStatsExtractor()

	statsA, hashA, sizeA, err := extractor.ExtractResponseData(responseBodyA, 50)
	require.NoError(t, err)

	statsB, hashB, sizeB, err := extractor.ExtractResponseData(responseBodyB, 55)
	require.NoError(t, err)

	cellAResp := &ResponseData{
		Body:       responseBodyA,
		StatusCode: 200,
		Duration:   50 * time.Millisecond,
		Stats:      statsA,
		Hash:       hashA,
		Size:       sizeA,
	}

	cellBResp := &ResponseData{
		Body:       responseBodyB,
		StatusCode: 200,
		Duration:   55 * time.Millisecond,
		Stats:      statsB,
		Hash:       hashB,
		Size:       sizeB,
	}

	// Process the query pair
	ctx := context.Background()
	manager.ProcessQueryPair(ctx, req, cellAResp, cellBResp)

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
	assert.Equal(t, hashA, sample.CellAResponseHash)
	assert.Equal(t, hashB, sample.CellBResponseHash)
	// Since the response content is identical, hashes should match
	assert.Equal(t, sample.CellAResponseHash, sample.CellBResponseHash)

	// Check the comparison result
	result := storage.results[0]
	assert.Equal(t, ComparisonStatusMatch, result.ComparisonStatus) // Same content = match
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
	manager, err := NewManager(config, storage, log.NewNopLogger(), prometheus.NewRegistry())
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

	// Extract stats for both responses
	extractor := NewStatsExtractor()

	statsA, hashA, sizeA, err := extractor.ExtractResponseData(responseBodyA, 50)
	require.NoError(t, err)

	statsB, hashB, sizeB, err := extractor.ExtractResponseData(responseBodyB, 50)
	require.NoError(t, err)

	cellAResp := &ResponseData{
		Body:       responseBodyA,
		StatusCode: 200,
		Duration:   50 * time.Millisecond,
		Stats:      statsA,
		Hash:       hashA,
		Size:       sizeA,
	}

	cellBResp := &ResponseData{
		Body:       responseBodyB,
		StatusCode: 200,
		Duration:   50 * time.Millisecond,
		Stats:      statsB,
		Hash:       hashB,
		Size:       sizeB,
	}

	ctx := context.Background()
	manager.ProcessQueryPair(ctx, req, cellAResp, cellBResp)

	time.Sleep(100 * time.Millisecond)

	assert.Len(t, storage.results, 1)
	result := storage.results[0]

	// Verify that different content produces different hashes
	assert.NotEqual(t, hashA, hashB, "Different content should produce different hashes")

	// Different hashes should result in mismatch
	assert.Equal(t, ComparisonStatusMismatch, result.ComparisonStatus)
	assert.Contains(t, result.DifferenceDetails, "content_hash")
}
