package goldfish

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// StatsExtractor extracts performance statistics and metadata from Loki responses.
// It provides privacy-compliant response analysis by computing content hashes
// and extracting performance metrics without storing sensitive data.
type StatsExtractor struct{}

// NewStatsExtractor creates a new statistics extractor.
// The extractor is stateless and can be safely reused across goroutines.
func NewStatsExtractor() *StatsExtractor {
	return &StatsExtractor{}
}

// ExtractResponseData extracts performance statistics and metadata from a Loki response.
// Returns QueryStats, content hash (FNV32), response size, and any parsing errors.
// The content hash excludes performance statistics to ensure identical content produces identical hashes.
func (e *StatsExtractor) ExtractResponseData(responseBody []byte, duration int64) (QueryStats, string, int64, error) {
	var queryResp loghttp.QueryResponse
	if err := json.Unmarshal(responseBody, &queryResp); err != nil {
		return QueryStats{}, "", 0, fmt.Errorf("failed to parse query response: %w", err)
	}

	// Extract statistics from the response
	queryStats := e.extractQueryStats(queryResp.Data.Statistics, duration)

	// Generate response hash for integrity checking (without storing sensitive data)
	responseHash := e.generateResponseHash(queryResp)

	// Calculate response size
	responseSize := int64(len(responseBody))

	return queryStats, responseHash, responseSize, nil
}

// extractQueryStats converts stats.Result to our QueryStats format
func (e *StatsExtractor) extractQueryStats(statsResult stats.Result, _ int64) QueryStats {
	return QueryStats{
		ExecTimeMs:           int64(statsResult.Summary.ExecTime * 1000),  // Convert seconds to milliseconds
		QueueTimeMs:          int64(statsResult.Summary.QueueTime * 1000), // Convert seconds to milliseconds
		BytesProcessed:       statsResult.Summary.TotalBytesProcessed,
		LinesProcessed:       statsResult.Summary.TotalLinesProcessed,
		BytesPerSecond:       statsResult.Summary.BytesProcessedPerSecond,
		LinesPerSecond:       statsResult.Summary.LinesProcessedPerSecond,
		TotalEntriesReturned: statsResult.Summary.TotalEntriesReturned,
		Splits:               statsResult.Summary.Splits,
		Shards:               statsResult.Summary.Shards,
	}
}

// hashableResponse represents the structure used for content hashing (excludes performance stats)
type hashableResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     any    `json:"result"`
	} `json:"data"`
}

// generateResponseHash creates a content-sensitive hash by removing only performance statistics
func (e *StatsExtractor) generateResponseHash(resp loghttp.QueryResponse) string {
	// Create a copy of the response without statistics (which contain timing info)
	hashableResp := hashableResponse{
		Status: resp.Status,
	}
	hashableResp.Data.ResultType = string(resp.Data.ResultType)
	hashableResp.Data.Result = resp.Data.Result

	// Generate hash from the response content (excluding statistics)
	responseBytes, err := json.Marshal(hashableResp)
	if err != nil {
		// Fallback to empty string if marshaling fails
		return ""
	}

	h := fnv.New32()
	h.Write(responseBytes)

	return strconv.FormatUint(uint64(h.Sum32()), 16)

}

// CompareStats compares two QueryStats and returns performance differences.
// Returns a map of metric names to comparison details including ratios where applicable.
// Division by zero is handled by omitting ratio calculations when the denominator is zero.
func (e *StatsExtractor) CompareStats(cellA, cellB QueryStats) map[string]any {
	differences := make(map[string]any)

	// Compare execution times
	if cellA.ExecTimeMs != cellB.ExecTimeMs {
		execTimeEntry := map[string]any{
			"cell_a_ms": cellA.ExecTimeMs,
			"cell_b_ms": cellB.ExecTimeMs,
		}
		if cellA.ExecTimeMs > 0 {
			execTimeEntry["ratio"] = float64(cellB.ExecTimeMs) / float64(cellA.ExecTimeMs)
		}
		differences["exec_time"] = execTimeEntry
	}

	// Compare queue times
	if cellA.QueueTimeMs != cellB.QueueTimeMs {
		queueTimeEntry := map[string]any{
			"cell_a_ms": cellA.QueueTimeMs,
			"cell_b_ms": cellB.QueueTimeMs,
		}
		if cellA.QueueTimeMs > 0 {
			queueTimeEntry["ratio"] = float64(cellB.QueueTimeMs) / float64(cellA.QueueTimeMs)
		}
		differences["queue_time"] = queueTimeEntry
	}

	// Compare bytes processed
	if cellA.BytesProcessed != cellB.BytesProcessed {
		bytesEntry := map[string]any{
			"cell_a": cellA.BytesProcessed,
			"cell_b": cellB.BytesProcessed,
		}
		if cellA.BytesProcessed > 0 {
			bytesEntry["ratio"] = float64(cellB.BytesProcessed) / float64(cellA.BytesProcessed)
		}
		differences["bytes_processed"] = bytesEntry
	}

	// Compare lines processed
	if cellA.LinesProcessed != cellB.LinesProcessed {
		linesEntry := map[string]any{
			"cell_a": cellA.LinesProcessed,
			"cell_b": cellB.LinesProcessed,
		}
		if cellA.LinesProcessed > 0 {
			linesEntry["ratio"] = float64(cellB.LinesProcessed) / float64(cellA.LinesProcessed)
		}
		differences["lines_processed"] = linesEntry
	}

	// Compare processing rates
	if cellA.BytesPerSecond != cellB.BytesPerSecond {
		bytesRateEntry := map[string]any{
			"cell_a": cellA.BytesPerSecond,
			"cell_b": cellB.BytesPerSecond,
		}
		if cellA.BytesPerSecond > 0 {
			bytesRateEntry["ratio"] = float64(cellB.BytesPerSecond) / float64(cellA.BytesPerSecond)
		}
		differences["bytes_per_second"] = bytesRateEntry
	}

	if cellA.LinesPerSecond != cellB.LinesPerSecond {
		linesRateEntry := map[string]any{
			"cell_a": cellA.LinesPerSecond,
			"cell_b": cellB.LinesPerSecond,
		}
		if cellA.LinesPerSecond > 0 {
			linesRateEntry["ratio"] = float64(cellB.LinesPerSecond) / float64(cellA.LinesPerSecond)
		}
		differences["lines_per_second"] = linesRateEntry
	}

	// Compare result counts
	if cellA.TotalEntriesReturned != cellB.TotalEntriesReturned {
		differences["entries_returned"] = map[string]any{
			"cell_a": cellA.TotalEntriesReturned,
			"cell_b": cellB.TotalEntriesReturned,
		}
	}

	// Compare query complexity
	if cellA.Splits != cellB.Splits {
		differences["splits"] = map[string]any{
			"cell_a": cellA.Splits,
			"cell_b": cellB.Splits,
		}
	}

	if cellA.Shards != cellB.Shards {
		differences["shards"] = map[string]any{
			"cell_a": cellA.Shards,
			"cell_b": cellB.Shards,
		}
	}

	return differences
}
