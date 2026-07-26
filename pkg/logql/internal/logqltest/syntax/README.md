# Editor syntax highlighting for `.logqltest` scripts

A TextMate grammar for the declarative test DSL described in [../README.md](../README.md). It is
packaged in the VS Code extension layout, which is supported by multiple IDEs.

The grammar highlights the DSL itself. The LogQL query on an `eval` line is deliberately
rendered as one uniform span rather than tokenized as LogQL. It binds to the `.logqltest`
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
