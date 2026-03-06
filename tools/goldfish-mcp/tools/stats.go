package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/tools/goldfish-mcp/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolGetStatistics = "get_statistics"
)

// GetStatisticsResponse represents the enriched response from the get_statistics tool
type GetStatisticsResponse struct {
	QueriesExecuted       int64               `json:"queriesExecuted"`
	EngineCoverage        float64             `json:"engineCoverage"`
	MatchingQueries       float64             `json:"matchingQueries"`
	PerformanceDifference float64             `json:"performanceDifference"`
	Breakdown             StatisticsBreakdown `json:"breakdown,omitempty"`
	TimeRange             TimeRangeInfo       `json:"timeRange"`
}

// StatisticsBreakdown contains counts by comparison status
// Note: The Goldfish API may not provide this breakdown directly,
// so we'll leave it optional for now and can populate it if the API supports it
type StatisticsBreakdown struct {
	Match    int `json:"match,omitempty"`
	Mismatch int `json:"mismatch,omitempty"`
	Error    int `json:"error,omitempty"`
	Partial  int `json:"partial,omitempty"`
}

// TimeRangeInfo contains the time range for the statistics
type TimeRangeInfo struct {
	From string `json:"from,omitempty"` // RFC3339 format
	To   string `json:"to,omitempty"`   // RFC3339 format
}

// GetStatisticsParams contains parameters for getting statistics
type GetStatisticsParams struct {
	From           string `json:"from"`           // RFC3339 timestamp
	To             string `json:"to"`             // RFC3339 timestamp
	UsesRecentData *bool  `json:"usesRecentData"` // Default: true
}

// RegisterGetStatisticsTool registers the get_statistics tool with the MCP server
func RegisterGetStatisticsTool(srv *mcp.Server, apiClient *client.Client) {
	inputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"from": {"type": "string", "description": "Start time in RFC3339 format (e.g. '2024-01-01T00:00:00Z')"},
			"to": {"type": "string", "description": "End time in RFC3339 format (e.g. '2024-01-02T00:00:00Z')"},
			"usesRecentData": {"type": "boolean", "description": "Whether to use recent data (default: true)"}
		}
	}`)

	tool := &mcp.Tool{
		Name:        ToolGetStatistics,
		Description: "Get aggregated statistics for pattern analysis. Returns overall metrics about query execution, engine coverage, matching rates, and performance differences.",
		InputSchema: inputSchema,
	}

	handler := func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse parameters
		var params GetStatisticsParams
		if len(request.Params.Arguments) > 0 {
			paramsJSON, err := json.Marshal(request.Params.Arguments)
			if err != nil {
				return newErrorResult(fmt.Sprintf("failed to marshal parameters: %v", err)), nil
			}
			if err := json.Unmarshal(paramsJSON, &params); err != nil {
				return newErrorResult(fmt.Sprintf("failed to parse parameters: %v", err)), nil
			}
		}

		// Build client parameters
		clientParams := client.StatsParams{
			UsesRecentData: true, // default
		}

		if params.UsesRecentData != nil {
			clientParams.UsesRecentData = *params.UsesRecentData
		}

		// Parse time parameters
		if params.From != "" {
			from, err := time.Parse(time.RFC3339, params.From)
			if err != nil {
				return newErrorResult(fmt.Sprintf("invalid 'from' time format: %v (expected RFC3339)", err)), nil
			}
			clientParams.From = from
		}
		if params.To != "" {
			to, err := time.Parse(time.RFC3339, params.To)
			if err != nil {
				return newErrorResult(fmt.Sprintf("invalid 'to' time format: %v (expected RFC3339)", err)), nil
			}
			clientParams.To = to
		}

		// Call API
		stats, err := apiClient.GetStatistics(ctx, clientParams)
		if err != nil {
			return newErrorResult(fmt.Sprintf("failed to get statistics: %v", err)), nil
		}

		// Build enriched response
		response := GetStatisticsResponse{
			QueriesExecuted:       stats.QueriesExecuted,
			EngineCoverage:        stats.EngineCoverage,
			MatchingQueries:       stats.MatchingQueries,
			PerformanceDifference: stats.PerformanceDifference,
			TimeRange: TimeRangeInfo{
				From: formatTimeForResponse(clientParams.From),
				To:   formatTimeForResponse(clientParams.To),
			},
		}

		// TODO: Add breakdown if the API provides it in the future
		// For now, we only have aggregate stats

		// Serialize response to JSON
		responseJSON, err := json.Marshal(response)
		if err != nil {
			return newErrorResult(fmt.Sprintf("failed to serialize response: %v", err)), nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(responseJSON)}},
		}, nil
	}

	srv.AddTool(tool, handler)
}

// formatTimeForResponse formats a time.Time to RFC3339 string for the response
// Returns empty string if time is zero
func formatTimeForResponse(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
