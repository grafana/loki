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

## Using Tools
- When using the `mcp__acp__Write` tool, write to a path within the current worktree to avoid sandboxing/permissions issues. This includes when writing plan files.

## Cursor Cloud specific instructions

### Environment overview
- **Go 1.25.7** (auto-downloaded via toolchain directive in `go.mod`); the Makefile references Go 1.26.0 for Docker build images, but the module's `go 1.25.7` directive is what matters locally.
- Build with `BUILD_IN_CONTAINER=false` to avoid Docker dependency. The Makefile defaults `BUILD_IN_CONTAINER=true`; override it or build directly with `go build`.
- `golangci-lint` (v2.10.1) and `faillint` (v1.15.0) must be on `PATH` for `make lint`. They are installed to `/usr/local/bin` and `$(go env GOPATH)/bin` respectively by the update script.

### Running Loki locally
Run Loki as a single-binary monolith with zero external dependencies:
```bash
go build -o ./cmd/loki/loki ./cmd/loki
./cmd/loki/loki -config.file=./cmd/loki/loki-local-config.yaml
```
Loki takes ~25-30 s after startup to report `ready` (ingester + pattern-ingester warm-up). Check with `curl http://localhost:3100/ready`.

### Running tests
- `go test -tags netgo ./pkg/...` runs unit tests without Docker. Add `-count=1` to disable caching.
- `make test` builds all binaries first, then runs tests with coverage. This is slower but comprehensive.
- Integration tests (`make test-integration`) start Loki components in-process; no Docker needed.

### Lint
- `make lint` requires `golangci-lint` and `faillint` on PATH. It runs `golangci-lint run`, then `faillint` with multiple import-path checks.
- To lint a specific package faster: `golangci-lint run -v --timeout=15m --build-tags=linux ./pkg/some/package/...`

### Gotchas
- The `.golangci.yml` declares `version: "2"` — this requires golangci-lint v2.x, not v1.x.
- Vendor directory is checked in; do not run `go mod vendor` unless intentionally updating dependencies.
- The `go.mod` has an `ignore ./tools/dev` directive; that submodule is not part of the main build.
