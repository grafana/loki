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
make lint                     # run all linters
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
