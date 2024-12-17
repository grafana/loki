[![GoDoc](https://godoc.org/github.com/richardartoul/molecule?status.png)](https://godoc.org/github.com/richardartoul/molecule)
[![C.I](https://github.com/richardartoul/molecule/workflows/Go/badge.svg)](https://github.com/richardartoul/molecule/actions)

# Molecule

Molecule is a Go library for parsing protobufs in an efficient and zero-allocation manner. The API is loosely based on [this excellent](https://github.com/buger/jsonparser) Go JSON parsing library.

This library is in alpha and the API could change. The current APIs are fairly low level, but additional helpers may be added in the future to make certain operations more ergonomic.

## Rationale

The standard `Unmarshal` protobuf interface in Go makes it difficult to manually control allocations when parsing protobufs. In addition, its common to only require access to a subset of an individual protobuf's fields. These issues make it hard to use protobuf in performance critical paths.

This library attempts to solve those problems by introducing a streaming, zero-allocation interface that allows users to have complete control over which fields are parsed, and how/when objects are allocated.

The downside, of course, is that `molecule` is more difficult to use (and easier to misuse) than the standard protobuf libraries so its recommended that it only be used in situations where performance is important. It is not a general purpose replacement for `proto.Unmarshal()`. It is recommended that users familiarize themselves with the [proto3 encoding](https://developers.google.com/protocol-buffers/docs/encoding) before attempting to use this library.

## Features

1. Unmarshal all protobuf primitive types with a streaming, zero-allocation API.
2. Support for iterating through protobuf messages in a streaming fashion.
3. Support for iterating through packed protobuf repeated fields (arrays) in a streaming fashion.

## Not Supported

1. Proto2 syntax (some things will probably work, but nothing is tested).
2. Repeated fields encoded not using the "packed" encoding (although in theory they can be parsed using this library, there just aren't any special helpers).
3. Map fields. It *should* be possible to parse maps using this library's API, but it would be a bid tedious. I plan on adding better support for this once I settle on a reasonable API.
4. Probably lots of other things.

## Examples

The [godocs](https://godoc.org/github.com/richardartoul/molecule) have numerous runnable examples.

## Attributions

This library is mostly a thin wrapper around other people's work:

1. The interface was inspired by this [jsonparser](https://github.com/buger/jsonparser) library.
2. The codec for interacting with protobuf streams was lifted from this [protobuf reflection library](https://github.com/jhump/protoreflect). The code was manually vendored instead of imported to reduce dependencies.

## Dependencies
The core `molecule` library has zero external dependencies. The `go.sum` file does contain some dependencies introduced from the tests package, however,
those *should* not be included transitively when using this library.
