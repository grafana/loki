# Loki Client Library

A lightweight client library for interacting with Loki, designed for external 
consumers like Grafana Alloy.

## Design Goals

- **Canonical implementation**: This module contains the source code for lightweight utilities
- **Minimal dependencies**: Only essential dependencies (Prometheus labels/encoding, not full server)
- **Code reuse**: The main `loki/v3` module imports from this module to avoid duplication
- **Stable API**: Semantic versioning with clear compatibility guarantees

## Architecture

This module follows the **dependency inversion principle**:

```
┌─────────────────┐
│   loki/v3       │  (Heavy module with OTel, Prometheus server, etc.)
│                 │
│  imports from   │
│       ↓         │
┌─────────────────┐
│  loki/client    │  (Lightweight base module)
│                 │
│  - constants    │  (Zero dependencies)
│  - encoding     │  (Prometheus TSDB encoding only)
│  - syntax       │  (Prometheus labels only)
│  - pattern      │  (Zero dependencies)
└─────────────────┘
```

**Key Points**:
- ✅ Client module = **source of truth** for lightweight code
- ✅ v3 module = imports from client, adds heavy dependencies
- ✅ No duplication - code lives in one place
- ✅ External consumers (like Alloy) import client directly

## Packages

- `types`: Documentation and type information for Loki push API
- `logql`: LogQL syntax parsing and pattern matching
  - `syntax`: Label parsing and LogQL expression parsing
  - `pattern`: Pattern matching for log lines (zero dependencies)
- `util`: Utility functions
  - `constants`: Loki constants (log levels, metric namespaces, etc.) - **zero dependencies**
  - `encoding`: Binary encoding/decoding utilities (depends on Prometheus TSDB encoding)
  - `log`: Minimal logging interface

## Dependencies

This module has **minimal dependencies**:

- `github.com/prometheus/prometheus/model/labels` - Lightweight labels package
- `github.com/prometheus/prometheus/tsdb/encoding` - Lightweight TSDB encoding package

**What this module does NOT include**:
- ❌ OpenTelemetry Collector (v1.35.0-v1.43.0)
- ❌ Full Prometheus server with all dependencies
- ❌ OTel Contrib packages
- ❌ Heavy infrastructure dependencies

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

When developing locally, the v3 module uses `replace` to point to this client module:

```go
replace github.com/grafana/loki/client => ./client
```

This ensures code changes in the client module are immediately available in v3.

## Contributing

This module is part of the Loki project. See the main [Loki repository](https://github.com/grafana/loki)
for contribution guidelines.
