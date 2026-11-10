# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`universal-translator` (`ut`) is a Go i18n library that wraps `github.com/go-playground/locales` (CLDR locale data) to provide translation registration, plural rule handling (cardinal, ordinal, range), and `{0}`-style parameter substitution. Translations can be registered programmatically or loaded from JSON files.

## Commands

```bash
# Run tests with race detection and coverage
make test

# Run benchmarks
make bench

# Lint
make lint    # or: golangci-lint run

# Run a single test
go test -run TestTranslatorAdd -race -v ./...
```

## Architecture

Flat package `ut` at the repo root — no sub-packages.

- **`universal_translator.go`** — `UniversalTranslator` struct: holds a `map[string]Translator` (keyed by lowercased locale) plus a fallback. Constructor: `New(fallback, ...supportedLocales)`. Methods: `FindTranslator`, `GetTranslator`, `AddTranslator`, `VerifyTranslations`.
- **`translator.go`** — `Translator` interface (embeds `locales.Translator`) and private `translator` impl. Registration: `Add`, `AddCardinal`, `AddOrdinal`, `AddRange`. Resolution: `T`, `C`, `O`, `R`. Each translation key is `interface{}` (string or int). Parameter substitution indexes are precomputed at registration time via `transText`.
- **`import_export.go`** — JSON import/export. `Export` writes per-locale JSON files. `Import` reads them (supports recursive directory traversal). `ImportByReader` for streaming.
- **`errors.go`** — Typed error structs: `ErrUnknownTranslation`, `ErrExistingTranslator`, `ErrConflictingTranslation`, `ErrRangeTranslation`, `ErrOrdinalTranslation`, `ErrCardinalTranslation`, `ErrMissingPluralTranslation`, `ErrMissingBracket`, `ErrBadParamSyntax`, `ErrMissingLocale`, `ErrBadPluralDefinition`.

Note: the private field names `cardinalTanslations`, `ordinalTanslations`, `rangeTanslations` are historical typos — do not "fix" them as it would break serialization/deserialization compatibility.

## Testing

Tests live at the repo root alongside source files. Test data JSON files are in `testdata/`. The library is goroutine-safe for reads once translations are registered — benchmarks in `benchmarks_test.go` test both serial and parallel `T()` calls.

## CI

GitHub Actions (`.github/workflows/workflow.yml`): Go 1.19.x matrix across ubuntu/macos/windows with race detection, coverage via goveralls, and golangci-lint.
