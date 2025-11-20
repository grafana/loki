# Loki Client Module

`github.com/grafana/loki/client` is a lightweight, dependency-conscious Go module that bundles the packages external agents such as Grafana Alloy need from the Loki code-base without dragging in the entire `loki/v3` module graph.  The module focuses on stable data types, utilities, and LogQL helpers so downstream projects can upgrade OpenTelemetry, Prometheus, or collector dependencies independently.

## Design Goals

- **Minimal dependencies** – only include what the exported packages require; no Loki server, Collector, or Prometheus server wiring.
- **Stable API surface** – exported packages are intended for long-term consumption and follow semantic versioning once tagged.
- **Drop-in familiarity** – package layouts mirror their counterparts under `pkg/` in Loki to keep migration straightforward.

## Provided Packages

| Package | Purpose |
| --- | --- |
| `types` | Aliases core Push API structs from `github.com/grafana/loki/pkg/push` so clients have a single import path. |
| `util/constants` | Shared labels, namespaces, and feature-gate constants. |
| `util/encoding` | Helpers for working with Prometheus TSDB encoders/decoders. |
| `util/log` | Minimal logging setup with Prometheus metrics. |
| `util/wal` | Thin helpers for reading Loki-compatible WAL segments. |
| `logql/log` | The LogQL pipeline, parsers, and pattern engine. |
| `logql/syntax` | The LogQL expression parser, lexer, and AST helpers. |

Internally the module also exposes a trimmed `logqlmodel` package for parse errors and well-known label names plus an `internal/util` helper package that carries only the matcher/regex helpers needed by LogQL.

## Usage

```go
import (
    lokipush "github.com/grafana/loki/client/types"
    "github.com/grafana/loki/client/logql/syntax"
)

req := &lokipush.PushRequest{Streams: []lokipush.Stream{ /* ... */ }}
expr, err := syntax.ParseExpr(`{app="agent"}`)
```

## Versioning & Guarantees

- The module will be tagged independently from `loki/v3` once stabilized (start at `v1.0.0`).
- Breaking changes follow semantic versioning.
- Dependencies are intentionally constrained; `go mod graph | grep -E '(opentelemetry|prometheus/prometheus)'` should only reflect the components required by LogQL.

## Contributing

Changes that affect shared APIs should be accompanied by tests and mentioned in the migration guide.  Keep imports pointed at `github.com/grafana/loki/client/...` to avoid reintroducing large dependency trees.
