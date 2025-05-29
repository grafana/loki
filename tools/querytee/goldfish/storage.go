package goldfish

import (
	"context"
	"time"
)

// Storage defines the interface for storing query samples and comparison results
type Storage interface {
	// StoreQuerySample stores a sampled query and its responses from both cells
	StoreQuerySample(ctx context.Context, sample *QuerySample) error

	// StoreComparisonResult stores the outcome of comparing responses
	StoreComparisonResult(ctx context.Context, result *ComparisonResult) error

	// Close closes the storage connection
	Close() error
}

// QuerySample represents a sampled query with performance stats from both cells
type QuerySample struct {
	CorrelationID string
	TenantID      string
	Query         string
	QueryType     string
	StartTime     time.Time
	EndTime       time.Time
	Step          time.Duration

	// Performance statistics instead of raw responses
	CellAStats QueryStats
	CellBStats QueryStats

	// Response metadata without sensitive content
	CellAResponseHash string
	CellBResponseHash string
	CellAResponseSize int64
	CellBResponseSize int64
	CellAStatusCode   int
	CellBStatusCode   int

	SampledAt time.Time
}

// QueryStats contains extracted performance statistics
type QueryStats struct {
	ExecTimeMs           int64 // Execution time in milliseconds
	QueueTimeMs          int64 // Queue time in milliseconds
	BytesProcessed       int64 // Total bytes processed
	LinesProcessed       int64 // Total lines processed
	BytesPerSecond       int64 // Bytes processed per second
	LinesPerSecond       int64 // Lines processed per second
	TotalEntriesReturned int64 // Number of result entries
	Splits               int64 // Number of splits
	Shards               int64 // Number of shards
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
