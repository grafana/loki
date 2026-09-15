# Working on the logqltest DSL

This package defines the declarative test DSL for LogQL metric and log-selection queries. For
**writing** `.logqltest` scripts see [`testdata/AGENTS.md`](testdata/AGENTS.md); this file is about
**changing the DSL itself**.

The DSL is described in four places that must stay in sync:

| What | Where |
|------|-------|
| Implementation | [`parser.go`](parser.go), [`runner.go`](runner.go) |
| Human documentation | [`README.md`](README.md) |
| Editor syntax highlighting | [`syntax/`](syntax/AGENTS.md) (`logqltest.tmLanguage.json`) |
| Authoring conventions | [`testdata/AGENTS.md`](testdata/AGENTS.md) |

## Any DSL change must update the syntax definition

Whenever you add, remove, or alter a DSL construct — a command, a clause such as
`[repeat …]` / `[metadata …]`, an `expect` annotation, sample notation, comment or quoting rules —
update `syntax/logqltest.tmLanguage.json` in the same change, along with `README.md`. The grammar is
not covered by `go test`, so it silently rots otherwise: scripts still pass while the new syntax
shows up unhighlighted or, worse, mis-highlighted.

For how to verify a grammar change, see [`syntax/AGENTS.md`](syntax/AGENTS.md).
