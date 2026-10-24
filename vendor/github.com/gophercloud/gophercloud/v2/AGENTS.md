# AGENTS.md

Quick reference guide for AI coding agents working in the Gophercloud repository.

**Project:** Gophercloud - Go SDK for OpenStack services
**Module:** `github.com/gophercloud/gophercloud/v2`
**Language:** Go (see version in [go.mod](go.mod))
**Stable Branch:** v2 (main development on `main`)

## Build, Test & Lint Commands

### Running Tests

**Unit tests (default):**
```bash
make unit
```

**Unit tests with verbose output:**
```bash
go test -v ./...
```

**Run single test by name:**
```bash
cd openstack/compute/v2/servers
go test -run TestCreateServer ./...
```

**Coverage:**
```bash
make coverage
```

**Acceptance tests (requires live OpenStack - may incur charges):**
```bash
make acceptance              # All services
make acceptance-compute      # Specific service
```

**Run single acceptance test:**
```bash
cd internal/acceptance/openstack/compute/v2
go test -timeout 60m -tags "acceptance" -run TestServersList
```

### Linting & Formatting

```bash
make lint     # Run golangci-lint in container (Docker/Podman)
make format   # Run gofmt with simplify flag
```

**Note:** If lint fails with SELinux errors, run:
```bash
chcon -Rt svirt_sandbox_file_t .
chcon -Rt svirt_sandbox_file_t ~/.cache/golangci-lint
```

## Code Style Guidelines

### Import Organization

Group imports in this order (separated by blank lines):
1. Standard library (alphabetically)
2. External dependencies (alphabetically)
3. Gophercloud internal packages (alphabetically)

Example:
```go
import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/gophercloud/gophercloud/v2"
    "github.com/gophercloud/gophercloud/v2/pagination"
)
```

### File Structure

Standard package structure under `openstack/<service>/<service_version>/<resource>/`:
- **`requests.go`** - HTTP request functions and OptsBuilder types
- **`results.go`** - Response structs and extraction methods
- **`urls.go`** - Endpoint URL construction helpers
- **`microversions.go`** - Microversion-specific types (when needed)
- **`testing/`** - Unit tests with HTTP mocking

### Naming Conventions

**Result receivers and variables:**
- Result method receiver: `r`
- Unmarshalled variable: `s`
- Request function return value: `r`

**OptsBuilder pattern:**
- Interface name: `<Action>OptsBuilder` (e.g., `CreateOptsBuilder`, `ListOptsBuilder`)
- Method for request body: `To<Resource><Action>Map` (e.g., `ToServerCreateMap`)
- Method for query string: `To<Resource><Action>Query` (e.g., `ToServerListQuery`)

Example:
```go
type CreateOptsBuilder interface {
    ToServerCreateMap() (map[string]interface{}, error)
}

type CreateOpts struct {
    Name string `json:"name"`
}

func (opts CreateOpts) ToServerCreateMap() (map[string]interface{}, error) {
    return gophercloud.BuildRequestBody(opts, "server")
}
```

### Types & Pointers

- **New response fields (microversions):** Use pointer types to allow nil-checking
- **Optional request fields:** Always use `omitempty` JSON tag
- **Required fields:** No `omitempty` tag

### Error Handling

- Use `gophercloud.Result` and `gophercloud.ErrResult` types
- Extract errors with `.ExtractErr()` method
- Return errors directly, don't wrap unless adding context

### Documentation

- **All struct fields** must have GoDoc comments
- **Microversion-dependent fields** must document required version in GoDoc
- **Package documentation** goes in `doc.go`
- Follow existing comment style in similar packages

Example:
```go
// This requires the client to be set to microversion 2.52 or later.
// Tags is the list of server tags.
Tags []string `json:"tags,omitempty"`
```

### Testing Requirements

**Unit tests (in `testing/` subdirectory):**
- Use `testhelper` package to mock HTTP
- `fakeServer := th.SetupHTTP()` / `defer fakeServer.Teardown()` for setup/teardown
- `fakeServer.Mux.HandleFunc()` to register mock endpoints
- Test ALL options (every field in request/response structs)
- Use assertion helpers from `testhelper/convenience.go` (value assertions) and `testhelper/http_responses.go` (HTTP request assertions)
- `Assert*` variants are fatal (`t.Fatalf`), `Check*` variants are non-fatal (`t.Errorf`)
- Assertion argument order is **expected first, actual second**: `th.AssertEquals(t, "expected_value", actual.Field)`

**Acceptance tests:**
- Located in `internal/acceptance/openstack/<service>/`
- Test against real OpenStack APIs
- Cover all operation variants

## Microversions

Set microversion on ServiceClient:
```go
client.Microversion = "2.52"
```

**Implementation rules:**
- **New request fields:** Must use `omitempty` + document microversion
- **New response fields:** Add as pointer types
- **Changed response types:** Create new structs in `microversions.go`

See `docs/MICROVERSIONS.md` for details.

## Pull Request Requirements

**Before opening PR:**
1. **GitHub issue must exist** with core contributor approval
2. **PR description must include:**
   - `For #<ISSUE_NUMBER>` reference
   - Link(s) to OpenStack source code (non-master branch) proving validity
3. **Keep PRs focused:** Group related operations together; avoid mixing unrelated changes
4. **Tests required:** Unit tests AND acceptance tests covering all options
5. **Work-in-progress:** Prefix title with `[wip]` until ready
6. **Dependencies:** Prefix with `[Pending #PRNUM]` if depends on another PR

**During review:**
- Do NOT squash commits (only append)
- Follow existing patterns in codebase
- Address all reviewer feedback

## Common Patterns

**Context usage:**
Always pass `context.Context` to API operations:
```go
servers.List(client, opts).EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
    // ...
})
```

**Pagination:**
```go
pager := servers.List(client, servers.ListOpts{})
err := pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
    servers, err := servers.ExtractServers(page)
    // process...
    return true, nil
})
```

## Key Reminders

- Module path: `github.com/gophercloud/gophercloud/v2` (note the `/v2`)
- Gophercloud does NOT validate microversion compatibility
- PRs target `main` branch, not `v2`
- Documentation auto-generated from GoDoc comments
