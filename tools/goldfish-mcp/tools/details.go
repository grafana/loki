package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/grafana/loki/v3/tools/goldfish-mcp/client"
	"github.com/grafana/loki/v3/tools/goldfish-mcp/enrichment"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolGetQueryDetails = "get_query_details"
)

// QueryDetailsResponse represents the complete response from the get_query_details tool
type QueryDetailsResponse struct {
	Query        goldfish.QuerySample             `json:"query"`
	Performance  enrichment.PerformanceComparison `json:"performance"`
	Difference   enrichment.DifferenceSummary     `json:"difference"`
	CellAResult  map[string]interface{}           `json:"cellAResult,omitempty"`
	CellBResult  map[string]interface{}           `json:"cellBResult,omitempty"`
	Traces       TracesInfo                       `json:"traces"`
	ResultsError string                           `json:"resultsError,omitempty"`
}

// GetQueryDetailsParams contains parameters for getting query details
type GetQueryDetailsParams struct {
	CorrelationID string `json:"correlationId"`
}

// RegisterGetQueryDetailsTool registers the get_query_details tool with the MCP server
func RegisterGetQueryDetailsTool(srv *mcp.Server, apiClient *client.Client) {
	inputSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"correlationId": {"type": "string", "description": "Correlation ID of the query to retrieve"}
		},
		"required": ["correlationId"]
	}`)

	tool := &mcp.Tool{
		Name:        ToolGetQueryDetails,
		Description: "Get complete query information including both cell result payloads for diff analysis. Combines query metadata with full response data from both cells.",
		InputSchema: inputSchema,
	}

	handler := func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse parameters
		var params GetQueryDetailsParams
		if len(request.Params.Arguments) == 0 {
			return newErrorResult("missing required parameter: correlationId"), nil
		}

		paramsJSON, err := json.Marshal(request.Params.Arguments)
		if err != nil {
			return newErrorResult(fmt.Sprintf("failed to marshal parameters: %v", err)), nil
		}
		if err := json.Unmarshal(paramsJSON, &params); err != nil {
			return newErrorResult(fmt.Sprintf("failed to parse parameters: %v", err)), nil
		}

		if params.CorrelationID == "" {
			return newErrorResult("correlationId cannot be empty"), nil
		}

		// Get query by correlation ID
		query, err := apiClient.GetQueryByCorrelationID(ctx, params.CorrelationID)
		if err != nil {
			return newErrorResult(fmt.Sprintf("failed to get query: %v", err)), nil
		}

		// Build base response
		response := QueryDetailsResponse{
			Query:       *query,
			Performance: enrichment.CalculatePerformanceComparison(query.CellAStats, query.CellBStats),
			Difference:  enrichment.CalculateDifferenceSummary(*query),
			Traces:      buildTracesInfo(*query),
		}

		// Try to get cell results if they exist
		// Check if result URIs are present
		if query.CellAResultURI == "" && query.CellBResultURI == "" {
			response.ResultsError = "Query results were not persisted to object storage. This can happen when result persistence is disabled in Goldfish config, or results have expired and been deleted. You can still see query metadata and statistics."
		} else {
			// Try to get Cell A result
			if query.CellAResultURI != "" {
				cellAResult, err := apiClient.GetResult(ctx, params.CorrelationID, "cell-a")
				if err != nil {
					// Don't fail the whole request, just note the error
					if response.ResultsError == "" {
						response.ResultsError = fmt.Sprintf("Failed to retrieve Cell A result: %v", err)
					} else {
						response.ResultsError += fmt.Sprintf(" | Failed to retrieve Cell A result: %v", err)
					}
				} else {
					response.CellAResult = cellAResult
				}
			}

			// Try to get Cell B result
			if query.CellBResultURI != "" {
				cellBResult, err := apiClient.GetResult(ctx, params.CorrelationID, "cell-b")
				if err != nil {
					// Don't fail the whole request, just note the error
					if response.ResultsError == "" {
						response.ResultsError = fmt.Sprintf("Failed to retrieve Cell B result: %v", err)
					} else {
						response.ResultsError += fmt.Sprintf(" | Failed to retrieve Cell B result: %v", err)
					}
				} else {
					response.CellBResult = cellBResult
				}
			}
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
