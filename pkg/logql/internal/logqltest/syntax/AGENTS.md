# Working on the logqltest syntax grammars

This directory holds two grammars that give `.logqltest` scripts editor syntax highlighting:

- The TextMate grammar, for VS Code and JetBrains IDEs (VS Code extension layout: `package.json`
  binds the `.logqltest` extension, `logqltest.tmLanguage.json` is the grammar).
- The Tree-sitter grammar in [`zed/`](zed), for Zed.

See [`README.md`](README.md) for how to install them.

The grammars highlight the DSL only — the LogQL query on an `eval` line is deliberately rendered as
one uniform span, so LogQL changes never require touching them. When you change a DSL construct,
both grammars must change with it: see [`../AGENTS.md`](../AGENTS.md).

## Verifying the TextMate grammar

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

## The Zed grammar

`zed/tree-sitter-logqltest/grammar.js` is the grammar and `zed/languages/logqltest/highlights.scm`
maps its nodes to Zed's highlight captures. Where the TextMate grammar and `../parser.go` disagree,
follow the parser: log lines are never unescaped, and a `#` outside quotes starts a comment, even
after `expect fail msg:`.

Zed compiles `src/parser.c` as it is and does not run the generator, so the generated `src`
directory is committed. After every change to `grammar.js`, regenerate and commit it. This needs
Node and the `tree-sitter` CLI (0.25 or later), neither of which the repository provides:

```
cd zed/tree-sitter-logqltest
tree-sitter generate
tree-sitter test                              # corpus tests and highlight tests
tree-sitter parse -q ../../../testdata/*.logqltest
```

The last command exits non-zero if any script has an `ERROR` or `MISSING` node, which is the
equivalent of an unrecognized token. `tree-sitter test` also loads `highlights.scm`, so a query that
names a node the grammar does not have fails there. Add a corpus test in `test/corpus` for the
construct you changed, and a highlight assertion in `test/highlight` if its highlighting changed.
Check the trees you write by hand: `tree-sitter test --update` accepts whatever the parser
produced.

Two more checks catch what the tests do not:

- **Uncaptured tokens.** Every leaf node of every `../testdata/*.logqltest` tree should be covered
  by a capture in `highlights.scm`. Run the query with `web-tree-sitter` to find the ones that are
  not.
- **Agreement with the TextMate grammar.** Tokenize the same scripts with both engines and compare
  the class of every character. Every difference should be one you can explain from `../parser.go`,
  like the two above; any other is a bug in one of the grammars.

A grammar that parses is not yet a grammar that highlights well: look at a real script in Zed
before you call a change done.
