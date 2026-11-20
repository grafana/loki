package main

import (
	"context"
	"time"
)

const statusSuccess = "success"

// PredictedCause represents the tool's best guess at why a mismatch occurred.
type PredictedCause string

const (
	// Phase 1 — re-running the query now yields identical results.
	PredictedCauseIngestionDelay PredictedCause = "ingestion_pipeline_delay"

	// Phase 2 — log count comparison results.
	PredictedCauseQueryExecBug     PredictedCause = "query_execution_bug"
	PredictedCauseLogCountMismatch PredictedCause = "log_count_mismatch"

	// Phase 3 causes — determined by detailed response analysis.
	PredictedCauseDataMismatch PredictedCause = "data_mismatch"
	PredictedCauseUnknown      PredictedCause = "unknown"
)

// MismatchEntry holds parsed data from a single query-tee mismatch log line.
type MismatchEntry struct {
	CorrelationID string
	Tenant        string
	Query         string
	QueryType     string
	StartTime     time.Time
	EndTime       time.Time
	MismatchCause string

	CellAResultURI string
	CellBResultURI string

	CellAStatusCode int
	CellBStatusCode int

	CellAEntriesReturned int64
	CellBEntriesReturned int64
	CellABytesProcessed  int64
	CellBBytesProcessed  int64
	CellALinesProcessed  int64
	CellBLinesProcessed  int64
	CellAExecTimeMs      int64
	CellBExecTimeMs      int64

	CellAUsedNewEngine bool
	CellBUsedNewEngine bool
}

// AnalysisResult captures the output from the analysis pipeline.
type AnalysisResult struct {
	MismatchEntry *MismatchEntry

	// Phase indicates which analysis phase determined the cause (1, 2, or 3).
	Phase int

	PredictedCause PredictedCause

	// Phase 1: re-query with original query.
	Phase1ComparisonMismatchCause string

	// Phase 2: log count comparison.
	Phase2CountQuery string
	Phase2CellACount int64
	Phase2CellBCount int64

	// Phase 3 detailed analysis (deep mode only).
	DetectedMismatchType string
	MismatchedItem       string
	Details              string
}

// Analyzer performs deep inspection of response payloads to detect specific
// types of mismatches. Used in Phase 3 (deep mode) after re-queries and
// count comparison.
type Analyzer interface {
	Name() string
	CanAnalyze(entry *MismatchEntry, resultType string) bool
	Analyze(ctx context.Context, entry *MismatchEntry, cellAResp, cellBResp []byte) ([]*AnalysisResult, error)
}
