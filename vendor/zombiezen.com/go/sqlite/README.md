# `zombiezen.com/go/sqlite`

[![Go Reference](https://pkg.go.dev/badge/zombiezen.com/go/sqlite.svg)][reference docs]

This package provides a low-level Go interface to [SQLite 3][].
It is a fork of [`crawshaw.io/sqlite`][] that uses [`modernc.org/sqlite`][],
a CGo-free SQLite package.
It aims to be a mostly drop-in replacement for `crawshaw.io/sqlite`.

This package deliberately does not provide a `database/sql` driver.
See [David Crawshaw's rationale][] for an in-depth explanation.
If you want to use `database/sql` with SQLite without CGo,
use `modernc.org/sqlite` directly.

[`crawshaw.io/sqlite`]: https://github.com/crawshaw/sqlite
[David Crawshaw's rationale]: https://crawshaw.io/blog/go-and-sqlite
[`modernc.org/sqlite`]: https://pkg.go.dev/modernc.org/sqlite
[reference docs]: https://pkg.go.dev/zombiezen.com/go/sqlite
[SQLite 3]: https://sqlite.org/

## Features

- Full SQLite functionality via `modernc.org/sqlite`,
  an automatically generated translation of the original C source code of SQLite into Go
- Builds with `CGO_ENABLED=0`,
  allowing cross-compiling and data race detection
- Allows access to SQLite-specific features
  like [blob I/O][] and [user-defined functions][]
- Includes a simple [schema migration package][]
- Utilities for [running embedded SQL scripts][ExecScriptFS] using the
  [Go 1.16 embedding feature][]
- A [`go fix`-like tool][migration docs] for migrating existing code using
  `crawshaw.io/sqlite`
- A [simple REPL][] for debugging

[blob I/O]: https://pkg.go.dev/zombiezen.com/go/sqlite#Blob
[ExecScriptFS]: https://pkg.go.dev/zombiezen.com/go/sqlite/sqlitex#ExecScriptFS
[Go 1.16 embedding feature]: https://pkg.go.dev/embed
[migration docs]: cmd/zombiezen-sqlite-migrate/README.md
[schema migration package]: https://pkg.go.dev/zombiezen.com/go/sqlite/sqlitemigration
[simple REPL]: https://pkg.go.dev/zombiezen.com/go/sqlite/shell
[user-defined functions]: https://pkg.go.dev/zombiezen.com/go/sqlite#Conn.CreateFunction

## Install

```shell
go get zombiezen.com/go/sqlite
```

While this library does not use CGo,
make sure that you are building for one of the [supported architectures][].

[supported architectures]: https://pkg.go.dev/modernc.org/sqlite#hdr-Supported_platforms_and_architectures

## Getting Started

```go
import (
  "fmt"

  "zombiezen.com/go/sqlite"
  "zombiezen.com/go/sqlite/sqlitex"
)

// ...

// Open an in-memory database.
conn, err := sqlite.OpenConn(":memory:", sqlite.OpenReadWrite)
if err != nil {
  return err
}
defer conn.Close()

// Execute a query.
err = sqlitex.ExecuteTransient(conn, "SELECT 'hello, world';", &sqlitex.ExecOptions{
  ResultFunc: func(stmt *sqlite.Stmt) error {
    fmt.Println(stmt.ColumnText(0))
    return nil
  },
})
if err != nil {
  return err
}
```

If you're creating a new application,
see the [package examples][] or the [reference docs][].

If you're looking to switch existing code that uses `crawshaw.io/sqlite`,
take a look at the [migration docs][].

[package examples]: https://pkg.go.dev/zombiezen.com/go/sqlite#pkg-examples

## License

[ISC](LICENSE)
