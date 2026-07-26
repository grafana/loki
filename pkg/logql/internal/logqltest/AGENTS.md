# Working on the logqltest DSL

This package defines the declarative test DSL for LogQL metric queries. For **writing** `.logqltest`
scripts see [`testdata/AGENTS.md`](testdata/AGENTS.md); this file is about **changing the DSL
itself**.

The DSL is described in four places that must stay in sync:

| What | Where |
|------|-------|
| Implementation | [`parser.go`](parser.go), [`runner.go`](runner.go) |
| Human documentation | [`README.md`](README.md) |
| Editor syntax highlighting | [`syntax/logqltest.tmLanguage.json`](syntax/logqltest.tmLanguage.json) |
| Authoring conventions | [`testdata/AGENTS.md`](testdata/AGENTS.md) |

## Any DSL change must update the syntax definition

Whenever you add, remove, or alter a DSL construct — a command, a clause such as
`[repeat …]` / `[metadata …]`, an `expect` annotation, sample notation, comment or quoting rules —
update `syntax/logqltest.tmLanguage.json` in the same change, along with `README.md` and the scope
table in [`syntax/README.md`](syntax/README.md).

The grammar is not covered by `go test`, so it silently rots otherwise: scripts still pass while the
new syntax shows up unhighlighted or, worse, mis-highlighted.

Keep the grammar's scope of concern narrow. It highlights the DSL only — the LogQL query on an
`eval` line is deliberately one uniform span, so LogQL changes never require touching it.

## Verifying the grammar

Editors use TextMate grammars, so a change is best checked by tokenizing with the same engine
(`vscode-textmate` + `vscode-oniguruma`) rather than by eye. Load the grammar the way an editor
does — read `syntax/package.json` and follow `contributes.grammars[].path` — then tokenize every
`testdata/*.logqltest` file plus a scratch file covering the construct you changed.

Two checks catch most regressions:

- **No unscoped tokens.** Any non-whitespace token whose innermost scope is still the root
  `source.logqltest` is one the grammar failed to recognize.
- **No runaway block.** A `begin`/`end` rule whose inner pattern can consume its own terminator
  (for example `\S+` swallowing a closing `]`) makes the block never end, so every following line
  inherits the wrong scope. A sudden change in token counts is the symptom.

Both are easy to miss when only spot-checking a few lines in the IDE.
