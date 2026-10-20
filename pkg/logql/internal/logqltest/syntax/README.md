# Editor syntax highlighting for `.logqltest` scripts

A TextMate grammar for the declarative test DSL described in [../README.md](../README.md). It is
packaged in the VS Code extension layout, which is supported by multiple IDEs. Zed does not read
TextMate grammars, so it has its own Tree-sitter grammar in [`zed/`](zed).

The grammars highlight the DSL itself. The LogQL query on an `eval` line is deliberately
rendered as one uniform span rather than tokenized as LogQL. They bind to the `.logqltest`
extension.

## GoLand / IntelliJ

1. Settings → Plugins: check that the bundled **TextMate Bundles** plugin is enabled.
2. Settings → Editor → **TextMate Bundles** → `+`, and select this `syntax` directory.
3. Apply, then reopen a `.logqltest` file.

The bundle path is recorded in the IDE configuration rather than in the project, so each developer
adds it once per machine. There is no project-level file that IntelliJ picks up automatically.

Colors follow the active theme's mapping for the grammar's TextMate scopes (the `*.logqltest`
scope names in `logqltest.tmLanguage.json`); tune them under Settings → Editor → Color Scheme →
TextMate.

## VS Code

Link (or copy) this directory into the extensions folder and reload the window:

```
ln -s "$PWD/pkg/logql/internal/logqltest/syntax" ~/.vscode/extensions/logqltest
```

## Zed

The `zed` directory is a Zed extension. Its Tree-sitter grammar, `zed/tree-sitter-logqltest`,
highlights the same constructs as the TextMate grammar.

1. Open the command palette and run **zed: install dev extension**.
2. Select the `zed` directory, then reopen a `.logqltest` file.

Zed compiles the grammar itself, from the repository named in `zed/extension.toml`. By default
that is `main` on GitHub, and Zed fetches the whole repository to get at the grammar, which is slow
the first time. To use your checkout instead, which is also how to try a grammar change, edit the
`[grammars.logqltest]` table locally and do not commit the edit:

```toml
repository = "file:///absolute/path/to/loki"
rev = "my-branch"
```

Zed fetches only committed state, so commit first. Zed builds in `zed/grammars` and reuses that
checkout, so a branch or tag name in `rev` stops following the branch after the first build and
Zed keeps compiling the old grammar. Either set `rev` to a commit (`git rev-parse HEAD`) and update
it after every commit, or delete `zed/grammars` before each rebuild. The same goes for the default
`main`: to pick up a newer upstream grammar, delete `zed/grammars` and rebuild from the Extensions
page. Delete it too if Zed reports that the grammar directory "is not a git clone", which means
`repository` changed after a build.

`zed/grammars` and `*.wasm` are Zed's build output and are ignored by git. With a `file://`
repository, `zed/grammars` holds a full copy of the repository.

Colors follow the active theme's mapping for the captures in
`zed/languages/logqltest/highlights.scm`.
