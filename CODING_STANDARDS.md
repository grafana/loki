# Coding standards

This document describes coding conventions for the Loki codebase. Most of these rules are enforced automatically by `make lint` (`golangci-lint`). Run it before submitting a pull request.

## Table of contents

- [Go imports](#go-imports)
- [Error handling](#error-handling)
- [Naming](#naming)
- [Testing](#testing)
- [Logging](#logging)
- [Forbidden packages](#forbidden-packages)

## Go imports

Imports must be grouped into three sections, separated by blank lines:

1. Standard library
2. External packages
3. Internal packages (`github.com/grafana/loki/...`)

```go
import (
    "context"
    "fmt"

    "github.com/go-kit/log"
    "github.com/prometheus/common/model"

    "github.com/grafana/loki/v3/pkg/logproto"
    "github.com/grafana/loki/v3/pkg/logql"
)
```

Use `goimports` (called by `make lint`) to sort and group imports automatically.

## Error handling

- Always check returned errors. The linter (`errcheck`) flags unchecked errors.
- Wrap errors with context using `fmt.Errorf("doing X: %w", err)` so callers can inspect them with `errors.Is` / `errors.As`.
- Do not swallow errors silently; if an error is intentionally ignored, document why.

## Naming

Follow standard Go naming conventions:

- Use `MixedCaps` for exported names, `mixedCaps` for unexported.
- Acronyms are consistently cased: `HTTPServer`, not `HttpServer`; `userID`, not `userId`.
- Interface names describing a single method typically end in `-er`: `Reader`, `Writer`, `Flusher`.
- Test helper functions that call `t.Fatal` accept `testing.TB`, not `*testing.T`, so they can be reused in benchmarks.

## Testing

- Prefer table-driven tests to reduce boilerplate and make adding cases easy.
- Place tests in a `_test` package (e.g., `package logql_test`) to test the public API by default. Use the same package only when testing unexported internals.
- Use `require` (not `assert`) for checks where a failure should stop the test immediately — this avoids misleading failures cascading from an initial error.
- Integration tests must be gated with the `integration` build tag and live under `./integration/`. Run them with `make test-integration`.

## Logging

Use `github.com/go-kit/log` (not `github.com/go-kit/kit/log`, which is deprecated). The `depguard` linter enforces this.

Structured log lines use key-value pairs:

```go
level.Info(logger).Log("msg", "starting ingester", "addr", addr, "component", "ingester")
```

- Use `"msg"` as the first key to describe the event.
- Keep values scalar where possible; avoid embedding structured data in a single string value.
- Error log lines include `"err", err`.

## Forbidden packages

The linter enforces the following import restrictions:

| Forbidden | Use instead |
|---|---|
| `github.com/go-kit/kit/log` | `github.com/go-kit/log` |
