# Loki Development Guide

## Build & Test Commands

```bash
make all                      # build all binaries
make loki                     # build loki only
make logcli                   # build logcli only
make test                     # run all unit tests
make test-integration         # run integration tests
go test ./...                 # run all tests with Go directly
go test -v ./pkg/logql/...    # run tests in specific package
go test -run TestName ./pkg/path  # run a specific test
make lint  # run all linters (use in CI-like environment)
make format                   # format code (gofmt and goimports)
```

## Code Style Guidelines
- Follow standard Go formatting (gofmt/goimports)
- Import order: standard lib, external packages, then Loki packages
- Error handling: Always check errors with `if err != nil { return ... }`
- Use structured logging with leveled logging (go-kit/log)
- Use CamelCase for exported identifiers, camelCase for non-exported 
- Document all exported functions, types, and variables
- Use table-driven tests when appropriate
- Follow Conventional Commits format: `<change type>: Your change`
- For frontend: use TypeScript, functional components, component composition
- Frontend naming: lowercase with dashes for directories (components/auth-wizard)

## Documentation Standards
- Follow the Grafana [Writers' Toolkit](https://grafana.com/docs/writers-toolkit/) Style Guide
- Use CommonMark flavor of markdown for documentation
- Create LIDs (Loki Improvement Documents) for large functionality changes
- Document upgrading steps in `docs/sources/setup/upgrade/_index.md`
- Preview docs locally with `make docs` from the `/docs` directory
- Include examples and clear descriptions for public APIs

## OTel Resource Attribute Storage

Loki receives OpenTelemetry logs via the `POST /otlp/v1/logs` endpoint (handler in `pkg/distributor/http.go`, parsing in `pkg/loghttp/push/otlp.go`).

### Storage strategy

Resource attributes are stored via two mechanisms controlled by `limits_config.otlp_config`:

1. **Index labels** — used for stream selection (`{service_name="auth"}`). By default, 17 well-known resource attributes are indexed (e.g. `service.name`, `k8s.namespace.name`, `deployment.environment`). Full list in `pkg/loghttp/push/otlp_config.go` (`DefaultOTLPResourceAttributesAsIndexLabels`).

2. **Structured metadata** — stored per log entry. All resource, scope, and log attributes that are not promoted to index labels end up here. Queryable with `| key="value"` syntax. In data objects (`pkg/dataobj/`), each unique metadata key becomes its own column in the logs section (`pkg/dataobj/sections/logs/`). The dataobj layer does not distinguish between resource, scope, or log attributes — it stores whatever structured metadata it receives.

Special fields (`trace_id`, `span_id`, `severity_number`, `severity_text`, `observed_timestamp`, `flags`) are always captured as structured metadata. `severity_text` can optionally be indexed via `severity_text_as_label: true`. Scope fields (`scope_name`, `scope_version`, `scope_dropped_attributes_count`) are also always stored as structured metadata when present.

### Attribute name conversion

Attribute names are sanitized via `prometheus/otlptranslator.LabelNamer`: dots and special characters become underscores (e.g. `service.name` → `service_name`). Nested maps use underscore-separated paths.

### Configuration

```yaml
limits_config:
  otlp_config:
    resource_attributes:
      ignore_defaults: false          # skip the 17 default index labels
      attributes_config:
        - action: index_label         # promote to stream label
          attributes: ["service.name"]
        - action: structured_metadata # store as entry metadata
          attributes: ["custom.attr"]
        - action: drop                # discard
          regex: "^temp_.*"
    scope_attributes:                 # only drop or structured_metadata
      - action: structured_metadata
        attributes: ["scope.key"]
    log_attributes:
      - action: index_label
        attributes: ["log.level"]
```

The first matching rule wins. Matching supports exact attribute names or regexes.

### Key code paths

- OTLP parsing: `pkg/loghttp/push/otlp.go` — `otlpToLokiPushRequest()`
- Config & defaults: `pkg/loghttp/push/otlp_config.go`
- Attribute-to-label conversion: `pkg/loghttp/push/otlp.go` — `attributeToLabels()`
- Per-tenant config: `pkg/validation/limits.go` — `OTLPConfig()`

## Data Objects (`pkg/dataobj/`)

Columnar storage format for logs in object storage. Multi-stream, multi-tenant files that combine logs, stream metadata, and index data. Ingested via Kafka consumers, compacted in the background. See `pkg/dataobj/README.md` for format details.

Key directories: `pkg/dataobj/consumer/` (ingestion), `pkg/dataobj/metastore/` (object discovery), `pkg/dataobj/compaction/` (merging), `pkg/dataobj/internal/dataset/` (columnar internals).

## Using Tools
- When using the `mcp__acp__Write` tool, write to a path within the current worktree to avoid sandboxing/permissions issues. This includes when writing plan files.
