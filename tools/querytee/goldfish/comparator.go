package goldfish

import (
	"net/http"
	"time"
)

// CompareResponses compares performance statistics and hashes from QuerySample
func CompareResponses(sample *QuerySample) ComparisonResult {
	result := ComparisonResult{
		CorrelationID:     sample.CorrelationID,
		DifferenceDetails: make(map[string]any),
		PerformanceMetrics: PerformanceMetrics{
			CellAQueryTime:  time.Duration(sample.CellAStats.ExecTimeMs) * time.Millisecond,
			CellBQueryTime:  time.Duration(sample.CellBStats.ExecTimeMs) * time.Millisecond,
			CellABytesTotal: sample.CellAResponseSize,
			CellBBytesTotal: sample.CellBResponseSize,
		},
		ComparedAt: time.Now(),
	}

	// Calculate ratios
	if sample.CellAStats.ExecTimeMs > 0 {
		result.PerformanceMetrics.QueryTimeRatio = float64(sample.CellBStats.ExecTimeMs) / float64(sample.CellAStats.ExecTimeMs)
	}
	if sample.CellAResponseSize > 0 {
		result.PerformanceMetrics.BytesRatio = float64(sample.CellBResponseSize) / float64(sample.CellAResponseSize)
	}

	// Check status codes first
	if sample.CellAStatusCode != sample.CellBStatusCode {
		result.ComparisonStatus = ComparisonStatusMismatch
		result.DifferenceDetails["status_code"] = map[string]any{
			"cell_a": sample.CellAStatusCode,
			"cell_b": sample.CellBStatusCode,
		}
		return result
	}

	// Both non-200 status codes are considered a match
	if sample.CellAStatusCode != http.StatusOK {
		result.ComparisonStatus = ComparisonStatusMatch
		return result
	}

	// Compare response hashes for content verification
	if sample.CellAResponseHash != sample.CellBResponseHash {
		result.ComparisonStatus = ComparisonStatusMismatch
		result.DifferenceDetails["content_hash"] = map[string]any{
			"cell_a": sample.CellAResponseHash,
			"cell_b": sample.CellBResponseHash,
		}
		return result
	}

	// If we reach here, hashes match and status codes are both 200
	// This means the content is identical, so it's a match
	result.ComparisonStatus = ComparisonStatusMatch

	// Still compare performance statistics for analysis, but don't change match status
	compareQueryStats(sample.CellAStats, sample.CellBStats, &result)

	return result
}

// compareQueryStats compares performance statistics between two queries
func compareQueryStats(statsA, statsB QueryStats, result *ComparisonResult) {
	const tolerance = 0.1 // 10% tolerance for performance differences

	// Compare execution times (record variance for analysis)
	if statsA.ExecTimeMs > 0 && statsB.ExecTimeMs > 0 {
		ratio := float64(statsB.ExecTimeMs) / float64(statsA.ExecTimeMs)
		if ratio > (1+tolerance) || ratio < (1-tolerance) {
			result.DifferenceDetails["exec_time_variance"] = map[string]any{
				"cell_a_ms": statsA.ExecTimeMs,
				"cell_b_ms": statsB.ExecTimeMs,
				"ratio":     ratio,
			}
		}
	}

	// Compare bytes processed (should be exactly the same for same query)
	if statsA.BytesProcessed != statsB.BytesProcessed {
		result.DifferenceDetails["bytes_processed"] = map[string]any{
			"cell_a": statsA.BytesProcessed,
			"cell_b": statsB.BytesProcessed,
		}
	}

	// Compare lines processed (should be exactly the same for same query)
	if statsA.LinesProcessed != statsB.LinesProcessed {
		result.DifferenceDetails["lines_processed"] = map[string]any{
			"cell_a": statsA.LinesProcessed,
			"cell_b": statsB.LinesProcessed,
		}
	}

	// Compare total entries returned (should be exactly the same for same query)
	if statsA.TotalEntriesReturned != statsB.TotalEntriesReturned {
		result.DifferenceDetails["entries_returned"] = map[string]any{
			"cell_a": statsA.TotalEntriesReturned,
			"cell_b": statsB.TotalEntriesReturned,
		}
	}
}
