package goldfish

import (
	"time"
)

// QuerySample represents a sampled query with performance stats from both cells
type QuerySample struct {
	CorrelationID   string        `json:"correlationId"`
	TenantID        string        `json:"tenantId"`
	User            string        `json:"user"`
	IsLogsDrilldown bool          `json:"isLogsDrilldown"`
	Query           string        `json:"query"`
	QueryType       string        `json:"queryType"`
	StartTime       time.Time     `json:"startTime"`
	EndTime         time.Time     `json:"endTime"`
	Step            time.Duration `json:"step"`

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
	CellASpanID       string `json:"cellASpanID"`
	CellBSpanID       string `json:"cellBSpanID"`

	// Result storage metadata
	CellAResultURI         string `json:"cellAResultURI"`
	CellBResultURI         string `json:"cellBResultURI"`
	CellAResultSize        int64  `json:"cellAResultSize"`
	CellBResultSize        int64  `json:"cellBResultSize"`
	CellAResultCompression string `json:"cellAResultCompression"`
	CellBResultCompression string `json:"cellBResultCompression"`

	// Query engine version tracking
	CellAUsedNewEngine bool `json:"cellAUsedNewEngine"`
	CellBUsedNewEngine bool `json:"cellBUsedNewEngine"`

	// Comparison outcome
	ComparisonStatus     ComparisonStatus `json:"comparisonStatus"`
	MatchWithinTolerance bool             `json:"matchWithinTolerance"`

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
	CorrelationID        string
	ComparisonStatus     ComparisonStatus
	MatchWithinTolerance bool
	DifferenceDetails    map[string]any
	PerformanceMetrics   PerformanceMetrics
	ComparedAt           time.Time
}

// ComparisonStatus represents the outcome of a comparison
type ComparisonStatus string

const (
	ComparisonStatusMatch    ComparisonStatus = "match"
	ComparisonStatusMismatch ComparisonStatus = "mismatch"
	ComparisonStatusError    ComparisonStatus = "error"
	ComparisonStatusPartial  ComparisonStatus = "partial"
)

// IsValid checks if the ComparisonStatus value is valid
func (cs ComparisonStatus) IsValid() bool {
	switch cs {
	case ComparisonStatusMatch, ComparisonStatusMismatch, ComparisonStatusError, ComparisonStatusPartial:
		return true
	default:
		return false
	}
}

// PerformanceMetrics contains performance comparison data
type PerformanceMetrics struct {
	CellAQueryTime  time.Duration
	CellBQueryTime  time.Duration
	QueryTimeRatio  float64
	CellABytesTotal int64
	CellBBytesTotal int64
	BytesRatio      float64
}

// QueryFilter contains filters for querying sampled queries
type QueryFilter struct {
	Tenant           string
	User             string
	IsLogsDrilldown  *bool // pointer to handle true/false/nil states
	UsedNewEngine    *bool // pointer to handle true/false/nil states
	ComparisonStatus ComparisonStatus
	From, To         time.Time
}

// StatsFilter contains filters for statistics queries
type StatsFilter struct {
	From           time.Time
	To             time.Time
	UsesRecentData bool // When false, exclude queries that touch data within the last 3h
}

// Statistics contains aggregated statistics across sampled queries
type Statistics struct {
	QueriesExecuted       int64   `json:"queriesExecuted"`       // Count of queries executed with new engine
	EngineCoverage        float64 `json:"engineCoverage"`        // Ratio of queries using new engine
	MatchingQueries       float64 `json:"matchingQueries"`       // Ratio of queries with matching responses
	PerformanceDifference float64 `json:"performanceDifference"` // Geometric mean of performance ratio
}
