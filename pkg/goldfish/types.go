package goldfish

import (
	"time"
)

// QuerySample represents a sampled query with performance stats from both cells
type QuerySample struct {
	CorrelationID string        `json:"correlationId"`
	TenantID      string        `json:"tenantId"`
	Query         string        `json:"query"`
	QueryType     string        `json:"queryType"`
	StartTime     time.Time     `json:"startTime"`
	EndTime       time.Time     `json:"endTime"`
	Step          time.Duration `json:"step"`

	// Performance statistics instead of raw responses
	CellAStats QueryStats `json:"cellAStats"`
	CellBStats QueryStats `json:"cellBStats"`

	// Response metadata without sensitive content
	CellAResponseHash string `json:"cellAResponseHash"`
	CellBResponseHash string `json:"cellBResponseHash"`
	CellAResponseSize int64  `json:"cellAResponseSize"`
	CellBResponseSize int64  `json:"cellBResponseSize"`
	CellAStatusCode   int    `json:"cellAStatusCode"`
	CellBStatusCode   int    `json:"cellBStatusCode"`
	CellATraceID      string `json:"cellATraceID"`
	CellBTraceID      string `json:"cellBTraceID"`

	// Query engine version tracking
	CellAUsedNewEngine bool `json:"cellAUsedNewEngine"`
	CellBUsedNewEngine bool `json:"cellBUsedNewEngine"`

	SampledAt time.Time `json:"sampledAt"`
}

// QueryStats contains extracted performance statistics
type QueryStats struct {
	ExecTimeMs           int64 `json:"execTimeMs"`           // Execution time in milliseconds
	QueueTimeMs          int64 `json:"queueTimeMs"`          // Queue time in milliseconds
	BytesProcessed       int64 `json:"bytesProcessed"`       // Total bytes processed
	LinesProcessed       int64 `json:"linesProcessed"`       // Total lines processed
	BytesPerSecond       int64 `json:"bytesPerSecond"`       // Bytes processed per second
	LinesPerSecond       int64 `json:"linesPerSecond"`       // Lines processed per second
	TotalEntriesReturned int64 `json:"totalEntriesReturned"` // Number of result entries
	Splits               int64 `json:"splits"`               // Number of splits
	Shards               int64 `json:"shards"`               // Number of shards
}

// ComparisonResult represents the outcome of comparing two responses
type ComparisonResult struct {
	CorrelationID      string
	ComparisonStatus   ComparisonStatus
	DifferenceDetails  map[string]interface{}
	PerformanceMetrics PerformanceMetrics
	ComparedAt         time.Time
}

// ComparisonStatus represents the outcome of a comparison
type ComparisonStatus string

const (
	ComparisonStatusMatch    ComparisonStatus = "match"
	ComparisonStatusMismatch ComparisonStatus = "mismatch"
	ComparisonStatusError    ComparisonStatus = "error"
	ComparisonStatusPartial  ComparisonStatus = "partial"
)

// PerformanceMetrics contains performance comparison data
type PerformanceMetrics struct {
	CellAQueryTime  time.Duration
	CellBQueryTime  time.Duration
	QueryTimeRatio  float64
	CellABytesTotal int64
	CellBBytesTotal int64
	BytesRatio      float64
}

// Constants for outcome filtering
const (
	OutcomeAll      = "all"
	OutcomeMatch    = "match"
	OutcomeMismatch = "mismatch"
	OutcomeError    = "error"
)
