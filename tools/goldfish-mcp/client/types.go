package client

import (
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

// QueryParams contains parameters for querying sampled queries
type QueryParams struct {
	Page             int
	PageSize         int
	Tenant           string
	User             string
	ComparisonStatus goldfish.ComparisonStatus
	UsedNewEngine    *bool
	From             time.Time
	To               time.Time
}

// QueriesResponse represents the API response containing query samples
type QueriesResponse struct {
	Queries     []goldfish.QuerySample `json:"queries"`
	Total       int                    `json:"total"`
	HasMore     bool                   `json:"hasMore"`
	CurrentPage int                    `json:"currentPage"`
	PageSize    int                    `json:"pageSize"`
}

// apiQueriesResponse is the internal type for deserializing the flat API response
type apiQueriesResponse struct {
	Queries     []APIQuerySample `json:"queries"`
	Total       int              `json:"total"`
	HasMore     bool             `json:"hasMore"`
	CurrentPage int              `json:"currentPage"`
	PageSize    int              `json:"pageSize"`
}

// toQueriesResponse converts the flat API response to the nested goldfish type
func (r *apiQueriesResponse) toQueriesResponse() *QueriesResponse {
	queries := make([]goldfish.QuerySample, len(r.Queries))
	for i, apiQuery := range r.Queries {
		queries[i] = apiQuery.ToGoldfishQuerySample()
	}
	return &QueriesResponse{
		Queries:     queries,
		Total:       r.Total,
		HasMore:     r.HasMore,
		CurrentPage: r.CurrentPage,
		PageSize:    r.PageSize,
	}
}

// StatsParams contains parameters for statistics queries
type StatsParams struct {
	From           time.Time
	To             time.Time
	UsesRecentData bool
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error string `json:"error"`
}

// APIQuerySample represents the flat JSON structure returned by the Goldfish API.
// This matches the actual API response format, unlike goldfish.QuerySample which
// expects nested CellAStats/CellBStats objects.
type APIQuerySample struct {
	CorrelationID   string        `json:"correlationId"`
	TenantID        string        `json:"tenantId"`
	User            string        `json:"user"`
	Issuer          string        `json:"issuer"`
	IsLogsDrilldown bool          `json:"isLogsDrilldown"`
	Query           string        `json:"query"`
	QueryType       string        `json:"queryType"`
	StartTime       time.Time     `json:"startTime"`
	EndTime         time.Time     `json:"endTime"`
	Step            time.Duration `json:"step"`

	// Cell A performance statistics (flat as returned by API)
	CellAExecTimeMs      int64 `json:"cellAExecTimeMs"`
	CellAQueueTimeMs     int64 `json:"cellAQueueTimeMs"`
	CellABytesProcessed  int64 `json:"cellABytesProcessed"`
	CellALinesProcessed  int64 `json:"cellALinesProcessed"`
	CellABytesPerSecond  int64 `json:"cellABytesPerSecond"`
	CellALinesPerSecond  int64 `json:"cellALinesPerSecond"`
	CellAEntriesReturned int64 `json:"cellAEntriesReturned"`
	CellASplits          int64 `json:"cellASplits"`
	CellAShards          int64 `json:"cellAShards"`

	// Cell B performance statistics (flat as returned by API)
	CellBExecTimeMs      int64 `json:"cellBExecTimeMs"`
	CellBQueueTimeMs     int64 `json:"cellBQueueTimeMs"`
	CellBBytesProcessed  int64 `json:"cellBBytesProcessed"`
	CellBLinesProcessed  int64 `json:"cellBLinesProcessed"`
	CellBBytesPerSecond  int64 `json:"cellBBytesPerSecond"`
	CellBLinesPerSecond  int64 `json:"cellBLinesPerSecond"`
	CellBEntriesReturned int64 `json:"cellBEntriesReturned"`
	CellBSplits          int64 `json:"cellBSplits"`
	CellBShards          int64 `json:"cellBShards"`

	// Response metadata
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
	ComparisonStatus     goldfish.ComparisonStatus `json:"comparisonStatus"`
	MatchWithinTolerance bool                      `json:"matchWithinTolerance"`

	SampledAt time.Time `json:"sampledAt"`

	// Trace and logs links
	CellATraceLink string `json:"cellATraceLink"`
	CellBTraceLink string `json:"cellBTraceLink"`
	CellALogsLink  string `json:"cellALogsLink"`
	CellBLogsLink  string `json:"cellBLogsLink"`
}

// ToGoldfishQuerySample converts the flat API structure to the nested goldfish.QuerySample
func (a *APIQuerySample) ToGoldfishQuerySample() goldfish.QuerySample {
	return goldfish.QuerySample{
		CorrelationID:   a.CorrelationID,
		TenantID:        a.TenantID,
		User:            a.User,
		Issuer:          a.Issuer,
		IsLogsDrilldown: a.IsLogsDrilldown,
		Query:           a.Query,
		QueryType:       a.QueryType,
		StartTime:       a.StartTime,
		EndTime:         a.EndTime,
		Step:            a.Step,
		CellAStats: goldfish.QueryStats{
			ExecTimeMs:           a.CellAExecTimeMs,
			QueueTimeMs:          a.CellAQueueTimeMs,
			BytesProcessed:       a.CellABytesProcessed,
			LinesProcessed:       a.CellALinesProcessed,
			BytesPerSecond:       a.CellABytesPerSecond,
			LinesPerSecond:       a.CellALinesPerSecond,
			TotalEntriesReturned: a.CellAEntriesReturned,
			Splits:               a.CellASplits,
			Shards:               a.CellAShards,
		},
		CellBStats: goldfish.QueryStats{
			ExecTimeMs:           a.CellBExecTimeMs,
			QueueTimeMs:          a.CellBQueueTimeMs,
			BytesProcessed:       a.CellBBytesProcessed,
			LinesProcessed:       a.CellBLinesProcessed,
			BytesPerSecond:       a.CellBBytesPerSecond,
			LinesPerSecond:       a.CellBLinesPerSecond,
			TotalEntriesReturned: a.CellBEntriesReturned,
			Splits:               a.CellBSplits,
			Shards:               a.CellBShards,
		},
		CellAResponseHash:      a.CellAResponseHash,
		CellBResponseHash:      a.CellBResponseHash,
		CellAResponseSize:      a.CellAResponseSize,
		CellBResponseSize:      a.CellBResponseSize,
		CellAStatusCode:        a.CellAStatusCode,
		CellBStatusCode:        a.CellBStatusCode,
		CellATraceID:           a.CellATraceID,
		CellBTraceID:           a.CellBTraceID,
		CellASpanID:            a.CellASpanID,
		CellBSpanID:            a.CellBSpanID,
		CellAResultURI:         a.CellAResultURI,
		CellBResultURI:         a.CellBResultURI,
		CellAResultSize:        a.CellAResultSize,
		CellBResultSize:        a.CellBResultSize,
		CellAResultCompression: a.CellAResultCompression,
		CellBResultCompression: a.CellBResultCompression,
		CellAUsedNewEngine:     a.CellAUsedNewEngine,
		CellBUsedNewEngine:     a.CellBUsedNewEngine,
		ComparisonStatus:       a.ComparisonStatus,
		MatchWithinTolerance:   a.MatchWithinTolerance,
		SampledAt:              a.SampledAt,
	}
}
