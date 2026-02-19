# Goldfish MCP Quick Start Guide

Get started with Goldfish MCP in 5 minutes.

## Prerequisites

- Go 1.21 or later installed
- kubectl access to a Kubernetes cluster running Loki with Goldfish enabled
- Claude Code CLI installed

## Step 1: Build the Binary

```bash
cd tools/goldfish-mcp
make build
```

Alternatively, you can build directly with Go:

```bash
go build -o goldfish-mcp .
```

## Step 2: Configure Claude Code

Create or edit `~/.claude/config.json`:

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

Replace `/path/to/loki` with the actual absolute path to your Loki repository.

## Step 3: Start Port-Forward

Open a terminal and run:

```bash
kubectl port-forward --context <context> --namespace <namespace> svc/query-frontend 3100:3100
```

Replace `<context>` with your Kubernetes context name (e.g., `ops-context`) and `<namespace>` with the namespace where Loki is deployed (e.g., `loki-ops`).

Leave this running. The port-forward must stay active while using Claude Code with Goldfish.

## Step 4: Start Claude Code

In a new terminal:

```bash
claude
```

## Step 5: Test the Connection

In Claude Code, ask:

```
Check if goldfish is accessible
```

Claude should respond with a success message indicating the API is accessible.

## Step 6: Start Exploring

Try these queries:

```
Show me recent query mismatches
```

```
What are the overall goldfish statistics?
```

```
Get details for correlation ID <id>
```

## Troubleshooting

### "Cannot connect to Goldfish API"

- Check that your port-forward is still running
- Verify the port number matches (default 3100)
- Ensure the service name is correct for your deployment

### "Goldfish feature is disabled"

- Verify Goldfish is enabled in your Loki configuration
- Make sure you're port-forwarding to the query-frontend service

### "Query results not available"

- Results may not be persisted if object storage isn't configured
- You can still see query metadata with the `list_queries` tool

## Next Steps

- Read the full [README.md](README.md) for detailed documentation
- Check [PLAN.md](PLAN.md) for architecture and implementation details
- Review example configurations in `config.example.json` and `config.advanced.json`

## Common Use Cases

### Finding Query Mismatches

```
User: Show me queries that mismatched in the last 2 hours

Claude: [calls list_queries with time filter and comparisonStatus="mismatch"]
```

### Analyzing a Specific Query

```
User: Why did correlation ID abc123 produce different results?

Claude: [calls get_query_details to get full comparison]
```

### Pattern Analysis

```
User: Are there any patterns in the mismatches for tenant=prod?

Claude: [calls list_queries with filters, analyzes patterns]
```

### Performance Comparison

```
User: Is the new engine faster or slower?

Claude: [calls get_statistics, analyzes performance metrics]
```

## Tips

1. Keep the port-forward running in a dedicated terminal
2. Use specific correlation IDs when investigating particular queries
3. Filter by tenant or user to narrow down results
4. Check statistics first to get an overview before diving into details
5. Use time ranges to focus on recent data

## Getting Help

For detailed information on:
- Available tools and their parameters: See [README.md](README.md#available-tools)
- Error messages and solutions: See [README.md](README.md#troubleshooting)
- Project architecture: See [PLAN.md](PLAN.md#architecture)
