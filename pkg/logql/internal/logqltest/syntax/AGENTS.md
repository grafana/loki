# Working on the logqltest syntax grammar

This directory holds the TextMate grammar that gives `.logqltest` scripts editor syntax
highlighting (VS Code extension layout: `package.json` binds the `.logqltest` extension,
`logqltest.tmLanguage.json` is the grammar). See [`README.md`](README.md) for how to install it.

The grammar highlights the DSL only — the LogQL query on an `eval` line is deliberately rendered as
one uniform span, so LogQL changes never require touching it. When you change a DSL construct, the
grammar must change with it: see [`../AGENTS.md`](../AGENTS.md).

## Verifying the grammar

Editors use TextMate grammars, so a change is best checked by tokenizing with the same engine
(`vscode-textmate` + `vscode-oniguruma`) rather than by eye. Load the grammar the way an editor
does — read `package.json` and follow `contributes.grammars[].path` — then tokenize every
`../testdata/*.logqltest` file plus a scratch file covering the construct you changed.

Two checks catch most regressions:

- **No unscoped tokens.** Any non-whitespace token whose innermost scope is still the root
  `source.logqltest` is one the grammar failed to recognize.
- **No runaway block.** A `begin`/`end` rule whose inner pattern can consume its own terminator
  (for example `\S+` swallowing a closing `]`) makes the block never end, so every following line
  inherits the wrong scope. A sudden change in token counts is the symptom.

Both are easy to miss when only spot-checking a few lines in the IDE.
