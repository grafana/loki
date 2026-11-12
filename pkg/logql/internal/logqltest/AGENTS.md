# Working on the logqltest DSL

This package defines the declarative test DSL for LogQL metric and log-selection queries. For
**writing** `.logqltest` scripts see [`testdata/AGENTS.md`](testdata/AGENTS.md); this file is about
**changing the DSL itself**.

The DSL is described in four places that must stay in sync:

| What | Where |
|------|-------|
| Implementation | [`parser.go`](parser.go), [`runner.go`](runner.go) |
| Human documentation | [`README.md`](README.md) |
| Editor syntax highlighting | `tools/syntax/logqltest` in the `grafana/loki-toolbox` repository |
| Authoring conventions | [`testdata/AGENTS.md`](testdata/AGENTS.md) |

## Any DSL change must update the syntax definition

Whenever you add, remove, or alter a DSL construct — a command, a clause such as
`[repeat …]` / `[metadata …]`, an `expect` annotation, sample notation, comment or quoting rules —
update `README.md` in the same change, and open a matching change for the grammars in
`tools/syntax/logqltest` in `grafana/loki-toolbox`. Nothing checks the grammars against the scripts,
so they silently rot otherwise: scripts still pass while the new syntax shows up unhighlighted or,
worse, mis-highlighted.

For how to verify a grammar change, see `AGENTS.md` in that directory.
