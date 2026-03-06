// Package enrichment provides data enrichment functions for Goldfish API responses
package enrichment

import "github.com/grafana/loki/v3/pkg/goldfish"

// PerformanceComparison contains performance metrics comparing Cell A and Cell B
type PerformanceComparison struct {
	CellAExecTimeMs     int64   `json:"cellAExecTimeMs"`
	CellBExecTimeMs     int64   `json:"cellBExecTimeMs"`
	ExecTimeDeltaMs     int64   `json:"execTimeDeltaMs"`     // B - A
	ExecTimeRatio       float64 `json:"execTimeRatio"`       // B / A
	BytesProcessedRatio float64 `json:"bytesProcessedRatio"` // B / A
	LinesProcessedRatio float64 `json:"linesProcessedRatio"` // B / A
}

// CalculatePerformanceComparison computes performance comparison metrics
// from cell A and cell B statistics
func CalculatePerformanceComparison(cellAStats, cellBStats goldfish.QueryStats) PerformanceComparison {
	pc := PerformanceComparison{
		CellAExecTimeMs: cellAStats.ExecTimeMs,
		CellBExecTimeMs: cellBStats.ExecTimeMs,
		ExecTimeDeltaMs: cellBStats.ExecTimeMs - cellAStats.ExecTimeMs,
	}

	// Calculate execution time ratio (B / A)
	if cellAStats.ExecTimeMs > 0 {
		pc.ExecTimeRatio = float64(cellBStats.ExecTimeMs) / float64(cellAStats.ExecTimeMs)
	}

	// Calculate bytes processed ratio (B / A)
	if cellAStats.BytesProcessed > 0 {
		pc.BytesProcessedRatio = float64(cellBStats.BytesProcessed) / float64(cellAStats.BytesProcessed)
	}

	// Calculate lines processed ratio (B / A)
	if cellAStats.LinesProcessed > 0 {
		pc.LinesProcessedRatio = float64(cellBStats.LinesProcessed) / float64(cellAStats.LinesProcessed)
	}

	return pc
}
