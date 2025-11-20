# Migration Guide: `loki/v3` ➜ `loki/client`

This guide explains how to migrate Alloy (and other consumers) from the full `github.com/grafana/loki/v3` module to the lightweight `github.com/grafana/loki/client` module.

## Why Migrate?

- Avoid the heavy OpenTelemetry Collector and Prometheus server dependencies declared by `loki/v3`.
- Decouple Alloy's dependency upgrades from Loki's release cadence.
- Consume only the packages that are designed for external use.

## Quick Steps

1. **Add the client module** to `go.mod`:
   ```bash
   go get github.com/grafana/loki/client@latest
   ```
2. **Update imports**:
   ```go
   - github.com/grafana/loki/v3/pkg/loghttp/push
   + github.com/grafana/loki/client/types

   - github.com/grafana/loki/v3/pkg/util/constants
   + github.com/grafana/loki/client/util/constants

   - github.com/grafana/loki/v3/pkg/util/encoding
   + github.com/grafana/loki/client/util/encoding

   - github.com/grafana/loki/v3/pkg/util/log
   + github.com/grafana/loki/client/util/log

   - github.com/grafana/loki/v3/pkg/util/wal
   + github.com/grafana/loki/client/util/wal

   - github.com/grafana/loki/v3/pkg/logql/syntax
   + github.com/grafana/loki/client/logql/syntax

   - github.com/grafana/loki/v3/pkg/logql/log/pattern
   + github.com/grafana/loki/client/logql/log/pattern
   ```
3. **Run `go mod tidy`** to drop the extraneous dependencies.

## Notable Differences

- `types` re-exports the push protobuf/go structs from `github.com/grafana/loki/pkg/push`, so functionality remains unchanged.
- `logqlmodel` moved to `github.com/grafana/loki/client/logqlmodel` and only exposes the symbols required by LogQL packages (parse errors, error label constants, etc.).
- Internal helpers that previously lived in `pkg/util` are now under `github.com/grafana/loki/client/internal/util` and are not intended for external imports.

## Validation

After updating imports:

```bash
# ensure the module stays lightweight
cd path/to/your/project
GOEXPERIMENT=loopvar go test ./...
go mod graph | grep -E '(opentelemetry|prometheus/prometheus)'  # should be empty or minimal
```

If a missing symbol or helper is discovered, open an issue or PR against Loki so we can add it to the client module without reopening the dependency floodgates.
