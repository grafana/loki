# Loki Client Library

A lightweight client library for interacting with Loki, designed for external 
consumers like Grafana Alloy.

## Design Goals

- **Code reuse**: Re-exports from the main `loki/v3` module to avoid duplication
- **Minimal API surface**: Only the types and utilities needed by external clients
- **Stable API**: Semantic versioning with clear compatibility guarantees

## Code Reuse Strategy

This module uses Go's `replace` directive to import from the parent `loki/v3` module,
ensuring code is maintained in one place without duplication. The client module
re-exports specific packages and functions while maintaining the same implementation.

**Important**: This module will have some dependencies (notably Prometheus `model/labels`
and `tsdb/encoding`) because the underlying v3 packages use them. However, these are
lightweight packages, not the full Prometheus server.

## Packages

- `types`: Documentation and type information for Loki push API
- `logql`: LogQL syntax parsing and pattern matching
  - `syntax`: Label parsing and LogQL expression parsing (re-exports from v3)
  - `pattern`: Pattern matching for log lines (copied, zero dependencies)
- `util`: Utility functions (re-exports from v3)
  - `constants`: Loki constants (log levels, metric namespaces, etc.)
  - `encoding`: Binary encoding/decoding utilities
  - `log`: Minimal logging interface

## Dependencies

This module imports from `github.com/grafana/loki/v3` to reuse code. As a result,
it will have some transitive dependencies including:

- `github.com/prometheus/prometheus/model/labels` - Lightweight labels package
- `github.com/prometheus/prometheus/tsdb/encoding` - Lightweight TSDB encoding package
- Various other dependencies from the v3 module

**However**, this module does NOT require:
- OpenTelemetry Collector (v1.35.0-v1.43.0)
- Full Prometheus server with all its dependencies
- OTel Contrib packages

The dependencies are significantly reduced compared to importing the full v3 module.

## Push API Types

The push API types (PushRequest, Stream, Entry) are defined in a separate module:
`github.com/grafana/loki/pkg/push`. This module already has minimal dependencies
and can be imported directly:

```go
import "github.com/grafana/loki/pkg/push"

req := &push.PushRequest{
    Streams: []push.Stream{
        {
            Labels: `{job="test"}`,
            Entries: []push.Entry{
                {
                    Timestamp: time.Now(),
                    Line:      "log line",
                },
            },
        },
    },
}
```

## Usage Examples

### Parsing Labels

```go
import "github.com/grafana/loki/client/logql/syntax"

labels, err := syntax.ParseLabels(`{job="test", instance="localhost"}`)
if err != nil {
    log.Fatal(err)
}

fmt.Println(labels.Get("job")) // "test"
```

### Pattern Matching

```go
import "github.com/grafana/loki/client/logql/pattern"

matcher, err := pattern.New(`<timestamp> <level> <message>`)
if err != nil {
    log.Fatal(err)
}

line := []byte("2024-01-01 INFO Hello world")
captures := matcher.Matches(line)
// captures[0] = "2024-01-01"
// captures[1] = "INFO"
// captures[2] = "Hello world"
```

### Using Constants

```go
import "github.com/grafana/loki/client/util"

fmt.Println(util.Loki)                    // "loki"
fmt.Println(util.LogLevelInfo)             // "info"
fmt.Println(util.AggregatedMetricLabel)    // "__aggregated_metric__"
```

## Versioning

This module follows semantic versioning independently of the main Loki module.
Breaking changes will only be introduced in major version bumps.

## Migration from loki/v3

See [MIGRATION.md](./MIGRATION.md) for detailed migration instructions.

## Development

When developing locally, this module uses `replace` to point to the parent v3 module:

```go
replace github.com/grafana/loki/v3 => ../
```

This ensures code changes in v3 are immediately available in the client module.

## Contributing

This module is part of the Loki project. See the main [Loki repository](https://github.com/grafana/loki)
for contribution guidelines.
