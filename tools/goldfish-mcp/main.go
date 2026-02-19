// goldfish-mcp provides MCP access to Loki Goldfish query comparison data.
// This server wraps the Goldfish API to enable AI-assisted debugging of query correctness mismatches.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/grafana/loki/v3/tools/goldfish-mcp/client"
	"github.com/grafana/loki/v3/tools/goldfish-mcp/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// Configure logging to stderr only - stdout is reserved for MCP protocol
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Parse configuration flags
	var cfg Config
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cfg.RegisterFlags(fs)

	// Customize help message
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goldfish-mcp [flags]\n\n")
		fmt.Fprintf(os.Stderr, "goldfish-mcp provides MCP access to Goldfish query comparison data.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  goldfish-mcp --api-url http://localhost:3100\n\n")
	}

	// Parse flags
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n\n", err)
		fs.Usage()
		os.Exit(1)
	}

	// Print configuration to stderr for troubleshooting
	fmt.Fprintf(os.Stderr, "goldfish-mcp v1.0.0\n")
	fmt.Fprintf(os.Stderr, "Configuration:\n")
	fmt.Fprintf(os.Stderr, "  API URL: %s\n", cfg.APIURL)
	fmt.Fprintf(os.Stderr, "  Timeout: %s\n\n", cfg.Timeout)

	// Initialize HTTP client
	apiClient := client.NewClient(cfg.APIURL, cfg.Timeout)

	// Create MCP server
	srv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "goldfish-mcp",
			Version: "1.0.0",
		},
		nil,
	)

	// Register all tools
	tools.RegisterHealthCheckTool(srv, apiClient, cfg.APIURL)
	tools.RegisterListQueriesTool(srv, apiClient)
	tools.RegisterGetQueryDetailsTool(srv, apiClient)
	tools.RegisterGetStatisticsTool(srv, apiClient)

	fmt.Fprintf(os.Stderr, "Registered %d MCP tools:\n", 4)
	fmt.Fprintf(os.Stderr, "  - %s\n", tools.ToolHealthCheck)
	fmt.Fprintf(os.Stderr, "  - %s\n", tools.ToolListQueries)
	fmt.Fprintf(os.Stderr, "  - %s\n", tools.ToolGetQueryDetails)
	fmt.Fprintf(os.Stderr, "  - %s\n", tools.ToolGetStatistics)
	fmt.Fprintf(os.Stderr, "\nServer starting...\n\n")

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start server with stdio transport
	// The Run method will block until the context is cancelled or an error occurs
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Server stopped.\n")
}
