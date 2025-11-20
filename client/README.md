# Loki Client Library

A lightweight client library for interacting with Loki, designed for external 
consumers like Grafana Alloy.

## Design Goals

- **Minimal dependencies**: No OpenTelemetry Collector or Prometheus server dependencies
- **Stable API**: Semantic versioning with clear compatibility guarantees
- **Focused scope**: Only the types and utilities needed by external clients

## Packages

- `types`: Documentation and type information for Loki push API
- `logql`: LogQL syntax parsing and pattern matching
  - `syntax`: Label parsing and basic LogQL types
  - `pattern`: Pattern matching for log lines
- `util`: Utility functions
  - `constants`: Loki constants (log levels, metric namespaces, etc.)
  - `encoding`: Binary encoding/decoding utilities
  - `log`: Minimal logging interface

## Dependencies

This module intentionally has minimal dependencies to avoid version conflicts
in downstream projects. It should have zero OpenTelemetry Collector or 
Prometheus server dependencies.

Current dependencies:
- `github.com/stretchr/testify` (test only)

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
fmt.Println(labels.String())   // {instance="localhost", job="test"}
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

### Logging Interface

```go
import "github.com/grafana/loki/client/util"

// Implement your own logger
type MyLogger struct{}

func (l MyLogger) Debug(msg string, kv ...interface{}) { /* ... */ }
func (l MyLogger) Info(msg string, kv ...interface{})  { /* ... */ }
func (l MyLogger) Warn(msg string, kv ...interface{})   { /* ... */ }
func (l MyLogger) Error(msg string, kv ...interface{}) { /* ... */ }

// Set as default
util.DefaultLogger = MyLogger{}
```

## Versioning

This module follows semantic versioning independently of the main Loki module.
Breaking changes will only be introduced in major version bumps.

## Migration from loki/v3

See [MIGRATION.md](./MIGRATION.md) for detailed migration instructions.

## Limitations

- **LogQL Syntax**: This module provides a simplified label parser. For full LogQL
  query parsing (including filters, aggregations, etc.), you may need to import
  the full `github.com/grafana/loki/v3/pkg/logql/syntax` package, which has more
  dependencies.

- **WAL Utilities**: WAL reading utilities are not included in this module to
  avoid Prometheus TSDB dependencies. If needed, import from the main Loki module
  or implement your own WAL reader.

## Contributing

This module is part of the Loki project. See the main [Loki repository](https://github.com/grafana/loki)
for contribution guidelines.
