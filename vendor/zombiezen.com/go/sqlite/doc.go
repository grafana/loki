// Copyright (c) 2018 David Crawshaw <david@zentus.com>
// Copyright (c) 2021 Roxy Light <roxy@zombiezen.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// SPDX-License-Identifier: ISC

/*
Package sqlite provides a Go interface to SQLite 3.

The semantics of this package are deliberately close to the [SQLite3 C API].
See the [official C API introduction] for an overview of the basics.

An SQLite connection is represented by a [*Conn].
Connections cannot be used concurrently.
A typical Go program will create a pool of connections
(e.g. by using [zombiezen.com/go/sqlite/sqlitex.NewPool]
to create a [*zombiezen.com/go/sqlite/sqlitex.Pool])
so goroutines can borrow a connection
while they need to talk to the database.

This package assumes SQLite will be used concurrently by the
process through several connections, so the build options for
SQLite enable multi-threading and the [shared cache].

The implementation automatically handles shared cache locking,
see the documentation on [Stmt.Step] for details.

The optional SQLite 3 extensions compiled in are:
session, FTS5, RTree, JSON1, and GeoPoly.

This is not a [database/sql] driver.
For helper functions to make it easier to execute statements,
see the [zombiezen.com/go/sqlite/sqlitex] package.

# Statement Caching

Statements are prepared with the [Conn.Prepare] and [Conn.PrepareTransient] methods.
When using [Conn.Prepare], statements are keyed inside a connection by the
original query string used to create them. This means long-running
high-performance code paths can write:

	stmt, err := conn.Prepare("SELECT ...")

After all the connections in a pool have been warmed up by passing
through one of these Prepare calls, subsequent calls are simply a
map lookup that returns an existing statement.

# Transactions

SQLite transactions can be managed manually with this package
by directly executing BEGIN / COMMIT / ROLLBACK or
SAVEPOINT / RELEASE / ROLLBACK statements,
but there are also helper functions available in [zombiezen.com/go/sqlite/sqlitex]:

  - [zombiezen.com/go/sqlite/sqlitex.Transaction]
  - [zombiezen.com/go/sqlite/sqlitex.ImmediateTransaction]
  - [zombiezen.com/go/sqlite/sqlitex.ExclusiveTransaction]
  - [zombiezen.com/go/sqlite/sqlitex.Save]

# Schema Migrations

For simple schema migration needs, see the [zombiezen.com/go/sqlite/sqlitemigration] package.

# User-Defined Functions

Use [Conn.CreateFunction] to register Go functions for use as [SQL functions].

# Streaming Blobs

The sqlite package supports the SQLite incremental I/O interface
for streaming blob data into and out of the the database
without loading the entire blob into a single []byte.
(This is important when working either with very large blobs,
or more commonly, a large number of moderate-sized blobs concurrently.)
See [Conn.OpenBlob] for more details.

# Deadlines and Cancellation

Every connection can have a done channel associated with it
using the [Conn.SetInterrupt] method.
This is typically the channel returned by a [context.Context.Done] method.

As database connections are long-lived,
the [Conn.SetInterrupt] method can be called multiple times
to reset the associated lifetime.

[official C API introduction]: https://www.sqlite.org/cintro.html
[shared cache]: https://www.sqlite.org/sharedcache.html
[SQL functions]: https://sqlite.org/appfunc.html
[SQLite3 C API]: https://www.sqlite.org/c3ref/intro.html.
*/
package sqlite
