# jsonlite

[![Go Reference](https://pkg.go.dev/badge/github.com/parquet-go/jsonlite.svg)](https://pkg.go.dev/github.com/parquet-go/jsonlite)

A lightweight JSON parser for Go, optimized for performance through careful
memory management.

## Motivation

Go's standard `encoding/json` package is designed for marshaling and
unmarshaling into Go structs. This works well when you know the schema ahead of
time, but becomes awkward when you need to traverse arbitrary JSON
structures—you end up with `map[string]interface{}` and type assertions
everywhere.

Packages like `github.com/tidwall/gjson` solve this with path-based queries,
but they re-scan the input for each query. If you're recursively walking a JSON
document, you pay the parsing cost repeatedly.

`jsonlite` takes a different approach: parse once, traverse many times. The
parser builds a lightweight in-memory index of the entire document in a single
pass. Once parsed, you can navigate the structure freely without re-parsing.

## Design

The core insight is that most JSON strings don't contain escape sequences. When
a string like `"hello"` appears in the input, we don't need to copy it—we can
just point directly into the original input buffer. Only strings with escapes
(like `"hello\nworld"`) require allocation for the unescaped result.

Each JSON value is represented by a `Value` struct containing just two machine
words: a pointer and a packed integer. The integer uses its high bits to store
the value's type (null, boolean, number, string, array, or object) and its low
bits for length information. This bit-packing means the overhead per value is
minimal—16 bytes on 64-bit systems regardless of the value type.

Arrays and objects are stored as contiguous slices, so iteration is
cache-friendly. Object fields are sorted by key during parsing, enabling binary
search for lookups.

## Trade-offs

This design optimizes for read-heavy workloads where you parse once and query
multiple times. If you only need to extract a single value from a large
document, a streaming parser or path-based query might be more appropriate.

The parser assumes the input remains valid for the lifetime of the parsed
values. If you're parsing from a buffer that gets reused, you'll need to copy
strings out before the buffer changes.

Numeric values are stored as their original string representation and parsed on
demand when you call `Int()`, `Float()`, etc. This avoids precision loss for
large integers and defers parsing cost until needed, but means repeated numeric
access will re-parse each time.
