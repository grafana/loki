# Compute tests

This directory contains test files written in a Domain Specific Language (DSL)
for testing compute package operations. The DSL provides a concise way to
specify test cases for columnar data operations.

Tests are run by the `TestCompute` function in `compute_test.go`.

## DSL grammar

```
Cases        := Case*
Case         := Function Datum* Selection? "->" Datum TERMINATOR
Datum        := TypedValue
Selection    := "select" ":" "[" Scalar* "]" 
TypedValue   := Type ":" Value

Type         := "bool" | "int64" | "uint64" | "utf8" | "null"

Value        := Scalar | Array
Scalar       := <literal> | "null"
Array        := "[" Scalar* "]"

TERMINATOR   := "\n"
```

Comments start with `#` and continue to the end of the line. All whitespace is
ignored.

### Data types

- **bool**: Boolean values (`true`, `false`, `null`)
- **int64**: Signed 64-bit integers (e.g., `-42`, `123`)
- **uint64**: Unsigned 64-bit integers (e.g., `0`, `456`)
- **utf8**: UTF-8 strings, must be quoted (e.g., `"hello"`, `"test string"`). Escape sequences are supported.
- **null**: Explicit null type for null-only values

## Adding new compute functions

When a new compute function is added, update `compute_test.go` to invoke the
proper function based on a given name.

Depending on the signature of the compute function being called, some functions
may need special argument handling, such as converting a `columnar.Datum` into a
compiled regular expression.

## Error testing

The DSL cannot be used to define test cases where the compute function is
expected to fail. These cases should instead be tested in Go test files.

## Advanced tests

Advanced compute functions, such as compute functions operating on record
batches or structs, are better left to the Go test files. They are more complex
and require more setup than the DSL can handle.
