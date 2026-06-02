# AGENTS.md

Instructions file for coding agents.

## Environment Setup

- Use the minimum Go version specified in [go.mod](go.mod)
- Warn the user about an old Go version
- The project uses GNU make for building binaries and images. See [Makefile](Makefile)

## Commands

### Tests

For fast feeback run tests using `go test` (or `gotest` if available).

```bash
go test -v ./...                               # run all tests with Go directly
go test -v ./pkg/...                           # run Loki package tests
go test -v ./pkg/<pkg>/...                     # run tests in specific package
go test -v -test.run TestName ./pkg/<pkg>/...  # run a specific test
```

For full verification use dedicated `make` targets (only when requested).

```
make test                                      # run full test suite
make test-integration                          # run integration test suite
make test-fuzz                                 # run fuzz tests
```

## Code Style

- Follow standard Go formatting (gofmt/goimports)
- Follow import order:
  1. standard lib
  2. external packages
  3. Loki packages (`github.com/grafana/loki/v3`)
- Error handling: Always check errors with `if err != nil { return ... }`
- Use structured logging with leveled logging (`github.com/go-kit/log`)
- Document all exported functions, types, and variables

### Comments

Write self-descriptive code first; reach for a comment only when the code cannot speak for itself. A comment must earn its place.

- Prefer clear names and small functions over explanatory comments. If you feel the need to explain *what* the code does, rename or refactor instead.
- A comment must have a good reason to exist. Good reasons:
  - Explain *why*, not *what*: intent, trade-offs, or constraints the code cannot convey.
  - Warn about non-obvious behavior, edge cases, gotchas, or invariants that must hold.
  - Reference an issue, spec, RFC, or external decision that motivates the code.
  - Required doc comments on exported identifiers (see above).
- Do not write comments that merely restate the code (e.g. `// increment i`, `// return the result`, `// loop over items`).
- Avoid long, overcomplicated narration. Keep comments short and to the point.
- Delete commented-out code and stale TODOs rather than leaving them behind.
- Never add a comment just to explain a change you made; that belongs in the commit message or PR description.

### Testing

- Always prefer `github.com/stretchr/testify/require` over `github.com/stretchr/testify/assert`
- Use table-driven tests when appropriate

## Commits and Pull Requests

- Always run Loki package tests `gotest -v ./pkg/...` before commiting
- Focus on "why", rather than "what" in the commit message and PR description
- Follow conventional commits format: `<type>(<scope>): Your change` (note the uppercase after colon)

## Documentation Standards

- Follow the Grafana [Writers' Toolkit](https://grafana.com/docs/writers-toolkit/) Style Guide
- Use CommonMark flavor of markdown for documentation
- Create LIDs (Loki Improvement Documents) for large functionality changes
- Document upgrading steps in `docs/sources/setup/upgrade/_index.md`
- Preview docs locally with `make docs` from the `/docs` directory
- Include examples and clear descriptions for public APIs

## Using Tools

- When using the `mcp__acp__Write` tool, write to a path within the current worktree to avoid sandboxing/permissions issues. This includes when writing plan files.
