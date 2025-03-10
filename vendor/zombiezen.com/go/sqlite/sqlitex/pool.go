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

package sqlitex

import (
	"context"
	"fmt"
	"sync"

	"zombiezen.com/go/sqlite"
)

// PoolOptions is the set of optional arguments to [NewPool].
type PoolOptions struct {
	// Flags is interpreted the same way as the argument to [sqlite.Open].
	// A Flags value of 0 defaults to:
	//
	//  - SQLITE_OPEN_READWRITE
	//  - SQLITE_OPEN_CREATE
	//  - SQLITE_OPEN_WAL
	//  - SQLITE_OPEN_URI
	Flags sqlite.OpenFlags

	// PoolSize sets an explicit size to the pool.
	// If less than 1, a reasonable default is used.
	PoolSize int

	// PrepareConn is called for each connection in the pool to set up functions
	// and other connection-specific state.
	PrepareConn ConnPrepareFunc
}

// Pool is a pool of SQLite connections.
// It is safe for use by multiple goroutines concurrently.
type Pool struct {
	free    chan *sqlite.Conn
	closed  chan struct{}
	prepare ConnPrepareFunc

	mu     sync.Mutex
	all    map[*sqlite.Conn]context.CancelFunc
	inited map[*sqlite.Conn]struct{}
}

// Open opens a fixed-size pool of SQLite connections.
//
// Deprecated: [NewPool] supports more options.
func Open(uri string, flags sqlite.OpenFlags, poolSize int) (pool *Pool, err error) {
	return NewPool(uri, PoolOptions{
		Flags:    flags,
		PoolSize: poolSize,
	})
}

// NewPool opens a fixed-size pool of SQLite connections.
func NewPool(uri string, opts PoolOptions) (pool *Pool, err error) {
	if uri == ":memory:" {
		return nil, strerror{msg: `sqlite: ":memory:" does not work with multiple connections, use "file::memory:?mode=memory&cache=shared"`}
	}

	poolSize := opts.PoolSize
	if poolSize < 1 {
		poolSize = 10
	}
	p := &Pool{
		free:    make(chan *sqlite.Conn, poolSize),
		closed:  make(chan struct{}),
		prepare: opts.PrepareConn,
	}
	defer func() {
		// If an error occurred, call Close outside the lock so this doesn't deadlock.
		if err != nil {
			p.Close()
		}
	}()

	flags := opts.Flags
	if flags == 0 {
		flags = sqlite.OpenReadWrite |
			sqlite.OpenCreate |
			sqlite.OpenWAL |
			sqlite.OpenURI
	}

	// TODO(maybe)
	// sqlitex_pool is also defined in package sqlite
	// const sqlitex_pool = sqlite.OpenFlags(0x01000000)
	// flags |= sqlitex_pool

	p.all = make(map[*sqlite.Conn]context.CancelFunc)
	for i := 0; i < poolSize; i++ {
		conn, err := sqlite.OpenConn(uri, flags)
		if err != nil {
			return nil, err
		}
		p.free <- conn
		p.all[conn] = func() {}
	}

	return p, nil
}

// Get returns an SQLite connection from the Pool.
//
// Deprecated: Use [Pool.Take] instead.
// Get is an alias for backward compatibility.
func (p *Pool) Get(ctx context.Context) *sqlite.Conn {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, _ := p.Take(ctx)
	return conn
}

// Take returns an SQLite connection from the Pool.
//
// If no connection is available,
// Take will block until at least one connection is returned with [Pool.Put],
// or until either the Pool is closed or the context is canceled.
// If no connection can be obtained
// or an error occurs while preparing the connection,
// an error is returned.
//
// The provided context is also used to control the execution lifetime of the connection.
// See [sqlite.Conn.SetInterrupt] for details.
//
// Applications must ensure that all non-nil Conns returned from Take
// are returned to the same Pool with [Pool.Put].
func (p *Pool) Take(ctx context.Context) (*sqlite.Conn, error) {
	select {
	case conn := <-p.free:
		ctx, cancel := context.WithCancel(ctx)
		conn.SetInterrupt(ctx.Done())

		p.mu.Lock()
		p.all[conn] = cancel
		inited := true
		if p.prepare != nil {
			_, inited = p.inited[conn]
		}
		p.mu.Unlock()

		if !inited {
			if err := p.prepare(conn); err != nil {
				p.put(conn)
				return nil, fmt.Errorf("get sqlite connection: %w", err)
			}

			p.mu.Lock()
			if p.inited == nil {
				p.inited = make(map[*sqlite.Conn]struct{})
			}
			p.inited[conn] = struct{}{}
			p.mu.Unlock()
		}

		return conn, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("get sqlite connection: %w", ctx.Err())
	case <-p.closed:
		return nil, fmt.Errorf("get sqlite connection: pool closed")
	}
}

// Put puts an SQLite connection back into the Pool.
//
// Put will panic if the conn was not originally created by p.
// Put(nil) is a no-op.
//
// Applications must ensure that all non-nil Conns returned from [Pool.Get]
// are returned to the same Pool with Put.
func (p *Pool) Put(conn *sqlite.Conn) {
	if conn == nil {
		// See https://github.com/zombiezen/go-sqlite/issues/17
		return
	}
	query := conn.CheckReset()
	if query != "" {
		panic(fmt.Sprintf(
			"connection returned to pool has active statement: %q",
			query))
	}
	p.put(conn)
}

func (p *Pool) put(conn *sqlite.Conn) {
	p.mu.Lock()
	cancel, found := p.all[conn]
	if found {
		p.all[conn] = func() {}
	}
	p.mu.Unlock()

	if !found {
		panic("sqlite.Pool.Put: connection not created by this pool")
	}

	conn.SetInterrupt(nil)
	cancel()
	p.free <- conn
}

// Close interrupts and closes all the connections in the Pool,
// blocking until all connections are returned to the Pool.
func (p *Pool) Close() (err error) {
	close(p.closed)

	p.mu.Lock()
	n := len(p.all)
	cancelList := make([]context.CancelFunc, 0, n)
	for conn, cancel := range p.all {
		cancelList = append(cancelList, cancel)
		p.all[conn] = func() {}
	}
	p.mu.Unlock()

	for _, cancel := range cancelList {
		cancel()
	}
	for closed := 0; closed < n; closed++ {
		conn := <-p.free
		if err2 := conn.Close(); err == nil {
			err = err2
		}
	}
	return
}

// A ConnPrepareFunc is called for each connection in a pool
// to set up connection-specific state.
// It must be safe to call from multiple goroutines.
//
// If the ConnPrepareFunc returns an error,
// then it will be called the next time the connection is about to be used.
// Once ConnPrepareFunc returns nil for a given connection,
// it will not be called on that connection again.
type ConnPrepareFunc func(conn *sqlite.Conn) error

type strerror struct {
	msg string
}

func (err strerror) Error() string { return err.msg }
