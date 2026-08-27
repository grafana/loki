# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository

`github.com/go-playground/validator/v10` — struct and field validation library based on struct tags. The module path ends in `/v10`; changes must preserve v10 API compatibility. Contributions require test coverage (see `.github/CONTRIBUTING.md`).

## Commands

```bash
make test              # go test -cover -race ./...
make lint              # installs golangci-lint v2.0.2 if missing, then runs it
make bench             # go test -run=NONE -bench=. -benchmem ./...

# Single test / subtest
go test -run TestName ./...
go test -run TestName/subtest_name ./...

# Single benchmark
go test -run=NONE -bench=BenchmarkFieldSuccess -benchmem ./...
```

Tests live alongside source in the package root (`validator_test.go`, `benchmarks_test.go`); run them from the repo root. Non-standard validators and translations are separate packages (`./non-standard/validators`, `./translations/...`) and are included by `./...`.

## Architecture

The library compiles validation tags into cached execution plans, then runs them against values via reflection. Understanding three layers is enough to be productive:

### 1. Registration & configuration — [validator_instance.go](validator_instance.go)
`Validate` is the singleton entry point. It holds:
- `validations` — tag → `FuncCtx` (the validator functions)
- `aliases` — shorthand tag → expanded tag expression
- `customFuncs` — `reflect.Type` → value extractor (for types like `sql.NullString` or anything implementing the `Valuer` interface, see [doc.go](doc.go))
- `structLevelFuncs` — struct-level validators
- `tagCache` and `structCache` — parsed-tag and parsed-struct caches (critical for performance; `Validate` is thread-safe and must be used as a singleton)

Options live in [options.go](options.go) (`WithRequiredStructEnabled`, `WithPrivateFieldValidation`, etc.). `WithRequiredStructEnabled` is the forward-compatible default users should adopt before v11.

### 2. Baked-in validators — [baked_in.go](baked_in.go)
All built-in tags (`required`, `email`, `uuid`, `oneof`, `gt`, cross-field `eqfield`, etc.) are registered here as `Func`/`FuncCtx` receiving a `FieldLevel` (see [field_level.go](field_level.go)). `restrictedTags` enumerates names that cannot be overridden. Cross-field validators resolve the other field via `fl.GetStructFieldOK*` against the parent struct captured in the execution context.

Adjacent data tables:
- [regexes.go](regexes.go), [postcode_regexes.go](postcode_regexes.go) — compiled regexps used by validators
- [country_codes.go](country_codes.go), [currency_codes.go](currency_codes.go), [language_codes.go](language_codes.go) — lookup tables

When adding a new tag: register it in `bakedInValidators` in [baked_in.go](baked_in.go), add its description to the table in [README.md](README.md), and add tests in [validator_test.go](validator_test.go).

### 3. Execution — [validator.go](validator.go) + [cache.go](cache.go)
- [cache.go](cache.go) parses struct tags into `cField` and `cTag` linked lists once per type and stores them in `structCache`/`tagCache`. `cTag.typeof` (`typeDefault`, `typeOmitEmpty`, `typeDive`, `typeStructOnly`, `typeOr`, etc.) tells the executor what to do.
- [validator.go](validator.go) contains the per-call `validate` struct (pooled via `sync.Pool`) and the `validateStruct` / `traverseField` mutual recursion. `ns`/`actualNs` are the accumulated dotted namespaces used in error paths; `dive`, `keys`, `endkeys` push/pop through slices/maps.

Public entry points live on `Validate`: `Struct`, `StructCtx`, `StructPartial`, `StructExcept`, `StructFiltered`, `Var`, `VarWithValue`, and their `Ctx` variants.

### Errors
[errors.go](errors.go) defines `ValidationErrors` (a `[]FieldError`) and `InvalidValidationError`. Per [README.md](README.md) and [doc.go](doc.go), callers type-assert `err.(validator.ValidationErrors)` after checking `err != nil`. Only `InvalidValidationError` signals misuse (e.g. passing a non-struct to `Struct`).

### Struct-level & field-level custom validators
[struct_level.go](struct_level.go) and [field_level.go](field_level.go) define the contexts passed to user-registered functions. Struct-level validators report errors via `StructLevel.ReportError` / `ReportValidationErrors` — they operate on the whole struct instead of a single field.

### Translations — [translations.go](translations.go) + [translations/](translations/)
Each locale under `translations/<locale>` registers a human-readable message per tag against a `ut.Translator`. `FieldError.Translate(trans)` consumes the registered `TranslationFunc`. When adding a new tag with a translation, add an entry in each locale package — translations are parallel packages, not a single table.

### Non-standard validators — [non-standard/validators/](non-standard/validators/)
Opt-in validators (`NotBlank`) users register manually. Keep niche or opinionated validators here rather than expanding `baked_in.go`.

## Performance-sensitive areas

Validation is on the hot path for many users. When touching [cache.go](cache.go), [validator.go](validator.go), or `baked_in.go` hot functions:
- Avoid allocations in the success path — several baked-in validators and the executor reuse pooled buffers (`validate.misc`, `str1`, `str2`).
- Benchmarks in [benchmarks_test.go](benchmarks_test.go) gate regressions; run `make bench` before and after significant changes and include results in the PR if they move.

## Conventions

- Go module minimum version is pinned in [go.mod](go.mod); don't lower it.
- Don't introduce new top-level exported types without need — most extension happens through `Register*` methods on `Validate`.
- `restrictedTags` (in [baked_in.go](baked_in.go)) is intentionally a denylist for aliases and registrations; adding a tag with one of those names will break the parser.
- Examples live under `_examples/` (underscore prefix excludes them from the module build). Keep them runnable as standalone `main` packages.
