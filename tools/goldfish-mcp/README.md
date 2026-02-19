# Goldfish MCP Server

A standalone MCP (Model Context Protocol) server that wraps the Goldfish API to enable Claude Code to analyze Loki query mismatches interactively.

## Overview

Goldfish MCP Server provides Claude Code with tools to:
- Check Goldfish API connectivity
- List and filter sampled queries by comparison status
- Get detailed query information including full result payloads
- Retrieve aggregated statistics for pattern analysis

The server enriches raw API data with performance comparisons and difference summaries to help Claude identify patterns in query correctness failures.

## Architecture

```
Claude Code (MCP Client)
    ↓ stdio transport
goldfish-mcp binary (local)
    ↓ HTTP client
http://localhost:3100/ui/api/v1/goldfish/... (via kubectl port-forward)
    ↓
Goldfish API in query-frontend pod
    ↓
Goldfish Storage (MySQL + optional object storage)
```

## Prerequisites

1. **Goldfish enabled in Loki**: Your Loki deployment must have Goldfish configured and running
2. **kubectl access**: Access to the Kubernetes cluster running Loki
3. **Go 1.21+**: Required to build the binary
4. **Claude Code**: The official Claude CLI tool

## Installation

### Build from Source

```bash
# Navigate to the goldfish-mcp directory
cd tools/goldfish-mcp

# Build the binary
make build

# The binary is now ready at ./goldfish-mcp
```

### Add to PATH (Optional)

```bash
# Move to a directory in your PATH
sudo mv goldfish-mcp /usr/local/bin/

# Or create a symlink
ln -s $(pwd)/goldfish-mcp /usr/local/bin/goldfish-mcp
```

## Configuration

### Configure Claude Code

Add the goldfish-mcp server to your Claude Code configuration file at `~/.claude/config.json`:

```json
{
  "mcpServers": {
    "goldfish": {
      "command": "/path/to/loki/tools/goldfish-mcp/goldfish-mcp",
      "args": ["--api-url", "http://localhost:3100"]
    }
  }
}
```

Replace `/path/to/loki` with the actual path to your Loki repository.

### Command-Line Flags

The goldfish-mcp binary supports the following flags:

```
--api-url string        Goldfish API base URL (default "http://localhost:3100")
--timeout duration      HTTP request timeout (default 30s)
--debug                 Enable debug logging to stderr
-h, --help             Show help message
```

Example with custom settings:

```json
{
  "mcpServers": {
    "goldfish": {
      "command": "/usr/local/bin/goldfish-mcp",
      "args": [
        "--api-url", "http://localhost:3100",
        "--timeout", "60s",
        "--debug"
      ]
    }
  }
}
```

## Usage

### 1. Establish Port-Forward

Before starting Claude Code, establish a port-forward to the Loki query-frontend service:

```bash
kubectl port-forward --context <your-context> --namespace <your-namespace> svc/query-frontend 3100:3100
```

Keep this running in a separate terminal while using Claude Code.

### 2. Start Claude Code

```bash
claude
```

Claude Code will automatically start the goldfish-mcp server and connect to it via stdio.

### 3. Use Goldfish Tools in Claude Code

Once connected, Claude can use the following tools:

#### Check API Health

```
User: Check if goldfish is accessible

Claude: [calls health_check tool]
✅ Goldfish API is accessible at http://localhost:3100
```

#### List Queries

```
User: Show me recent query mismatches

Claude: [calls list_queries with comparisonStatus="mismatch"]
Found 15 mismatches in the last hour...
```

#### Get Query Details

```
User: Get details for correlation ID abc123

Claude: [calls get_query_details with correlationId="abc123"]
Here's the complete comparison between Cell A and Cell B...
```

#### View Statistics

```
User: What's the overall goldfish statistics?

Claude: [calls get_statistics]
Based on recent data, 92% of queries match between engines...
```

## Available Tools

### 1. `health_check`

Verifies Goldfish API connectivity.

**Parameters:** None

**Returns:**
- `status`: "ok" or "error"
- `apiUrl`: The configured API URL
- `message`: Human-readable status message

**Example:**
```json
{
  "status": "ok",
  "apiUrl": "http://localhost:3100",
  "message": "✅ Goldfish API is accessible"
}
```

### 2. `list_queries`

Lists goldfish queries with filtering options.

**Parameters:**
- `page` (optional): Page number, default 1
- `pageSize` (optional): Results per page, default 50, max 1000
- `tenant` (optional): Filter by tenant ID
- `user` (optional): Filter by user ID
- `comparisonStatus` (optional): "match", "mismatch", "error", or "partial"
- `usedNewEngine` (optional): Filter by engine version (boolean)
- `from` (optional): Start time in RFC3339 format
- `to` (optional): End time in RFC3339 format

**Returns:**
- `queries`: Array of query samples with enriched data
- `pagination`: Pagination metadata

**Enrichments:**
- `performance`: Execution time deltas and ratios
- `difference`: Boolean comparisons for status, size, and hash
- `traces`: Formatted trace and log links for both cells

### 3. `get_query_details`

Gets complete query information including both cell result payloads.

**Parameters:**
- `correlationId` (required): The correlation ID of the query

**Returns:**
- `query`: Complete query metadata
- `performance`: Execution time and processing comparisons
- `difference`: Detailed difference summary
- `cellAResult`: Full decompressed JSON response from Cell A
- `cellBResult`: Full decompressed JSON response from Cell B
- `traces`: Trace IDs, span IDs, and links for both cells

**Note:** This tool makes 3 API calls and combines them into a single response.

### 4. `get_statistics`

Gets aggregated statistics for pattern analysis.

**Parameters:**
- `from` (optional): Start time in RFC3339 format
- `to` (optional): End time in RFC3339 format
- `usesRecentData` (optional): Use recent data cache, default true

**Returns:**
- `queriesExecuted`: Total queries sampled
- `engineCoverage`: Percentage using new engine
- `matchingQueries`: Percentage of matching queries
- `performanceDifference`: Average performance ratio (new/old)
- `breakdown`: Counts by comparison status
- `timeRange`: Actual time range of data

## Troubleshooting

### Connection Errors

**Error Message:**
```
Error: Cannot connect to Goldfish API at http://localhost:3100/ui/api/v1/goldfish/...

Please ensure the port-forward is active:
  kubectl port-forward --context <your-context> --namespace <your-namespace> svc/query-frontend 3100:3100

Connection error: dial tcp [::1]:3100: connect: connection refused
```

**Solution:**
1. Check that your port-forward is running
2. Verify the service name matches your deployment (`loki-query-frontend` may differ)
3. Ensure you're forwarding the correct port (default is 3100)

### Goldfish Disabled

**Error Message:**
```
Error: Goldfish API returned error (HTTP 404)

Response: {"error": "goldfish feature is disabled"}
```

**Solution:**
1. Verify Goldfish is enabled in your Loki configuration
2. Check you're connecting to the query-frontend service (not querier or another component)
3. Restart Loki if you just enabled Goldfish

### Results Not Available

**Error Message:**
```
Error: Query results not available for correlation ID abc123

Reason: Results were not persisted to object storage.

This can happen when:
  1. Result persistence is disabled in Goldfish config
  2. The query was sampled before persistence was enabled
  3. Results have expired and been deleted
```

**Solution:**
- Use `list_queries` to see query metadata and statistics
- Enable result persistence in Goldfish configuration if needed
- Check object storage configuration (S3, GCS, etc.)

### Invalid Configuration

**Error Message:**
```
Error: --timeout must be a positive duration (got 0s)
```

**Solution:**
Check your command-line flags or Claude Code configuration for invalid values.

## Examples

### Find Recent Mismatches

```
User: Show me queries that mismatched in the last hour

Claude: [calls list_queries with comparisonStatus="mismatch", appropriate time range]
```

### Analyze Specific Query

```
User: Why did correlation ID xyz789 fail?

Claude: [calls get_query_details with correlationId="xyz789"]
[Analyzes the difference between Cell A and Cell B results]
```

### Performance Analysis

```
User: Are there performance differences between engines?

Claude: [calls get_statistics]
[Analyzes performanceDifference metric and breakdown]
```

### Pattern Detection

```
User: What patterns do you see in mismatches for tenant=prod?

Claude: [calls list_queries with tenant="prod", comparisonStatus="mismatch"]
[Analyzes the queries array to identify common patterns]
```

## Limitations

- Requires manual port-forward setup
- No authentication support (assumes localhost access)
- No result caching (all data fetched from API on each request)
- No automatic retry logic for transient failures

## Related Documentation

- [Model Context Protocol Specification](https://modelcontextprotocol.io/)
- [Claude Code Documentation](https://docs.anthropic.com/claude/docs/claude-code)

## License

This tool is part of the Grafana Loki project and follows the same license.
