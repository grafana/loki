package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/grafana/loki/v3/tools/goldfish-mcp/client"
	"github.com/grafana/loki/v3/tools/goldfish-mcp/enrichment"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolListQueries = "list_queries"
)

// EnrichedQuerySample contains a query sample with enriched data
type EnrichedQuerySample struct {
	goldfish.QuerySample
	Performance enrichment.PerformanceComparison `json:"performance"`
	Difference  enrichment.DifferenceSummary     `json:"difference"`
	Traces      TracesInfo                       `json:"traces"`
}

// TracesInfo contains formatted trace and logs links for both cells
type TracesInfo struct {
	CellA TraceLinks `json:"cellA"`
	CellB TraceLinks `json:"cellB"`
}

// TraceLinks contains formatted URLs for traces and logs
type TraceLinks struct {
	TraceID   string `json:"traceId,omitempty"`
	SpanID    string `json:"spanId,omitempty"`
	TraceLink string `json:"traceLink,omitempty"`
	LogsLink  string `json:"logsLink,omitempty"`
}

// ListQueriesResponse represents the response from the list_queries tool
type ListQueriesResponse struct {
	Queries    []EnrichedQuerySample `json:"queries"`
	Pagination PaginationInfo        `json:"pagination"`
}

// PaginationInfo contains pagination metadata
type PaginationInfo struct {
	Page         int  `json:"page"`
	PageSize     int  `json:"pageSize"`
	HasMore      bool `json:"hasMore"`
	TotalResults int  `json:"totalResults"`
}

// ListQueriesParams contains parameters for listing queries
type ListQueriesParams struct {
	Page             int    `json:"page"`
	PageSize         int    `json:"pageSize"`
	Tenant           string `json:"tenant"`
	User             string `json:"user"`
	ComparisonStatus string `json:"comparisonStatus"`
	UsedNewEngine    *bool  `json:"usedNewEngine"`
	From             string `json:"from"` // RFC3339 timestamp
	To               string `json:"to"`   // RFC3339 timestamp
}

// RegisterListQueriesTool registers the list_queries tool with the MCP server
func RegisterListQueriesTool(srv *mcp.Server, apiClient *client.Client) {
	inputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"page": {"type": "number", "description": "Page number (default: 1)"},
			"pageSize": {"type": "number", "description": "Results per page (default: 50, max: 1000)"},
			"tenant": {"type": "string", "description": "Filter by tenant ID"},
			"user": {"type": "string", "description": "Filter by user"},
			"comparisonStatus": {"type": "string", "description": "Filter by comparison status: 'match', 'mismatch', 'error', or 'partial'"},
			"usedNewEngine": {"type": "boolean", "description": "Filter by engine version (true = new engine, false = old engine)"},
			"from": {"type": "string", "description": "Start time in RFC3339 format (e.g. '2024-01-01T10:00:00Z')"},
			"to": {"type": "string", "description": "End time in RFC3339 format (e.g. '2024-01-01T11:00:00Z')"}
		}
	}`)

	tool := &mcp.Tool{
		Name:        ToolListQueries,
		Description: "List goldfish queries with filtering. Typically used to find mismatches or errors. Returns enriched query data with performance comparisons and trace links.",
		InputSchema: inputSchema,
	}

	handler := func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse parameters
		var params ListQueriesParams
		if len(request.Params.Arguments) > 0 {
			paramsJSON, err := json.Marshal(request.Params.Arguments)
			if err != nil {
				return newErrorResult(fmt.Sprintf("failed to marshal parameters: %v", err)), nil
			}
			if err := json.Unmarshal(paramsJSON, &params); err != nil {
				return newErrorResult(fmt.Sprintf("failed to parse parameters: %v", err)), nil
			}
		}

		// Set defaults
		if params.Page == 0 {
			params.Page = 1
		}
		if params.PageSize == 0 {
			params.PageSize = 50
		}
		if params.PageSize > 1000 {
			params.PageSize = 1000
		}

		// Build client parameters
		clientParams := client.QueryParams{
			Page:             params.Page,
			PageSize:         params.PageSize,
			Tenant:           params.Tenant,
			User:             params.User,
			ComparisonStatus: goldfish.ComparisonStatus(params.ComparisonStatus),
			UsedNewEngine:    params.UsedNewEngine,
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
		result, err := apiClient.GetQueries(ctx, clientParams)
		if err != nil {
			return newErrorResult(fmt.Sprintf("failed to get queries: %v", err)), nil
		}

		// Enrich queries
		enrichedQueries := make([]EnrichedQuerySample, len(result.Queries))
		for i, q := range result.Queries {
			enrichedQueries[i] = EnrichedQuerySample{
				QuerySample: q,
				Performance: enrichment.CalculatePerformanceComparison(q.CellAStats, q.CellBStats),
				Difference:  enrichment.CalculateDifferenceSummary(q),
				Traces:      buildTracesInfo(q),
			}
		}

		// Build response
		response := ListQueriesResponse{
			Queries: enrichedQueries,
			Pagination: PaginationInfo{
				Page:         result.CurrentPage,
				PageSize:     result.PageSize,
				HasMore:      result.HasMore,
				TotalResults: result.Total,
			},
		}

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

// buildTracesInfo creates formatted trace links from query sample data
func buildTracesInfo(q goldfish.QuerySample) TracesInfo {
	return TracesInfo{
		CellA: TraceLinks{
			TraceID: q.CellATraceID,
			SpanID:  q.CellASpanID,
			// TODO: Format actual trace links if we have Tempo/Grafana URLs configured
			// For now, just return the IDs
		},
		CellB: TraceLinks{
			TraceID: q.CellBTraceID,
			SpanID:  q.CellBSpanID,
		},
	}
}

// newErrorResult creates an error result for MCP tool responses
func newErrorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}
