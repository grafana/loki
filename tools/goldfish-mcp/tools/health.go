package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/loki/v3/tools/goldfish-mcp/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolHealthCheck = "health_check"
)

// HealthCheckResponse represents the response from the health check tool
type HealthCheckResponse struct {
	Status  string `json:"status"`
	APIUrl  string `json:"apiUrl"`
	Message string `json:"message"`
}

// RegisterHealthCheckTool registers the health_check tool with the MCP server
func RegisterHealthCheckTool(srv *mcp.Server, apiClient *client.Client, apiURL string) {
	tool := &mcp.Tool{
		Name:        ToolHealthCheck,
		Description: "Verify Goldfish API connectivity. Returns status information about the connection to the Goldfish API.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Perform health check
		err := apiClient.HealthCheck(ctx)

		var response HealthCheckResponse
		response.APIUrl = apiURL

		if err != nil {
			response.Status = "error"
			response.Message = fmt.Sprintf("Cannot reach Goldfish API: %v", err)
		} else {
			response.Status = "ok"
			response.Message = fmt.Sprintf("Goldfish API is accessible at %s", apiURL)
		}

		// Serialize response to JSON
		responseJSON, err := json.Marshal(response)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to serialize response: %v", err)}},
			}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(responseJSON)}},
		}, nil
	}

	srv.AddTool(tool, handler)
}
