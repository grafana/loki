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

package sqlite

import (
	"errors"
	"fmt"
	"io"
	"unsafe"

	"modernc.org/libc"
	lib "modernc.org/sqlite/lib"
)

const blobBufSize = 4096

var (
	mainCString = mustCString("main")
	tempCString = mustCString("temp")
)

// OpenBlob opens a blob in a particular {database,table,column,row}.
//
// https://www.sqlite.org/c3ref/blob_open.html
func (c *Conn) OpenBlob(dbn, table, column string, row int64, write bool) (*Blob, error) {
	if c == nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: nil connection", table, column)
	}
	return c.openBlob(dbn, table, column, row, write)
}

func (c *Conn) openBlob(dbn, table, column string, row int64, write bool) (_ *Blob, err error) {
	cdb, freeCDB, err := cDBName(dbn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
	}
	defer freeCDB()
	var writeFlag int32
	if write {
		writeFlag = 1
	}
	buf, err := malloc(c.tls, blobBufSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
	}
	defer func() {
		if err != nil {
			libc.Xfree(c.tls, buf)
		}
	}()

	ctable, err := libc.CString(table)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
	}
	defer libc.Xfree(c.tls, ctable)
	ccolumn, err := libc.CString(column)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
	}
	defer libc.Xfree(c.tls, ccolumn)

	blobPtrPtr, err := malloc(c.tls, ptrSize)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
	}
	defer libc.Xfree(c.tls, blobPtrPtr)
	for {
		if err := c.interrupted(); err != nil {
			return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
		}
		res := ResultCode(lib.Xsqlite3_blob_open(
			c.tls,
			c.conn,
			cdb,
			ctable,
			ccolumn,
			row,
			writeFlag,
			blobPtrPtr,
		))
		switch res {
		case ResultLockedSharedCache:
			if err := waitForUnlockNotify(c.tls, c.conn, c.unlockNote).ToError(); err != nil {
				return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, err)
			}
			// loop
		case ResultOK:
			blobPtr := *(*uintptr)(unsafe.Pointer(blobPtrPtr))
			return &Blob{
				conn: c,
				blob: blobPtr,
				buf:  buf,
				size: lib.Xsqlite3_blob_bytes(c.tls, blobPtr),
			}, nil
		default:
			return nil, fmt.Errorf("sqlite: open blob %q.%q: %w", table, column, c.extreserr(res))
		}
	}
}

// Blob provides streaming access to SQLite blobs.
type Blob struct {
	conn *Conn
	blob uintptr
	buf  uintptr
	off  int32
	size int32
}

func (blob *Blob) bufSlice() []byte {
	return libc.GoBytes(blob.buf, blobBufSize)
}

// Read reads up to len(p) bytes from the blob into p.
// https://www.sqlite.org/c3ref/blob_read.html
func (blob *Blob) Read(p []byte) (int, error) {
	if blob.blob == 0 {
		return 0, fmt.Errorf("sqlite: read blob: %w", errInvalidBlob)
	}
	if blob.off >= blob.size {
		return 0, io.EOF
	}
	if err := blob.conn.interrupted(); err != nil {
		return 0, fmt.Errorf("sqlite: read blob: %w", err)
	}
	if rem := blob.size - blob.off; len(p) > int(rem) {
		p = p[:rem]
	}
	fullLen := len(p)
	for len(p) > 0 {
		nn := int32(blobBufSize)
		if int(nn) > len(p) {
			nn = int32(len(p))
		}
		res := ResultCode(lib.Xsqlite3_blob_read(blob.conn.tls, blob.blob, blob.buf, nn, blob.off))
		if err := res.ToError(); err != nil {
			return fullLen - len(p), fmt.Errorf("sqlite: read blob: %w", err)
		}
		copy(p, blob.bufSlice()[:int(nn)])
		p = p[nn:]
		blob.off += nn
	}
	return fullLen, nil
}

// WriteTo copies the blob to w until there's no more data to write or
// an error occurs.
func (blob *Blob) WriteTo(w io.Writer) (n int64, err error) {
	if blob.blob == 0 {
		return 0, fmt.Errorf("sqlite: read blob: %w", errInvalidBlob)
	}
	if blob.off >= blob.size {
		return 0, nil
	}
	if err := blob.conn.interrupted(); err != nil {
		return 0, fmt.Errorf("sqlite: read blob: %w", err)
	}
	for blob.off < blob.size {
		buf := blob.bufSlice()
		if remaining := int(blob.size - blob.off); len(buf) > remaining {
			buf = buf[:remaining]
		}
		res := ResultCode(lib.Xsqlite3_blob_read(blob.conn.tls, blob.blob, blob.buf, int32(len(buf)), blob.off))
		if err := res.ToError(); err != nil {
			return n, fmt.Errorf("sqlite: read blob: %w", err)
		}
		nn, err := w.Write(buf)
		blob.off += int32(nn)
		n += int64(nn)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Write writes len(p) from p to the blob.
// https://www.sqlite.org/c3ref/blob_write.html
func (blob *Blob) Write(p []byte) (int, error) {
	if blob.blob == 0 {
		return 0, fmt.Errorf("sqlite: write blob: %w", errInvalidBlob)
	}
	if err := blob.conn.interrupted(); err != nil {
		return 0, fmt.Errorf("sqlite: write blob: %w", err)
	}
	fullLen := len(p)
	for len(p) > 0 {
		nn := copy(blob.bufSlice(), p)
		res := ResultCode(lib.Xsqlite3_blob_write(blob.conn.tls, blob.blob, blob.buf, int32(nn), blob.off))
		if err := res.ToError(); err != nil {
			return fullLen - len(p), fmt.Errorf("sqlite: write blob: %w", err)
		}
		p = p[nn:]
		blob.off += int32(nn)
	}
	return fullLen, nil
}

// WriteString writes s to the blob.
// https://www.sqlite.org/c3ref/blob_write.html
func (blob *Blob) WriteString(s string) (int, error) {
	if blob.blob == 0 {
		return 0, fmt.Errorf("sqlite: write blob: %w", errInvalidBlob)
	}
	if err := blob.conn.interrupted(); err != nil {
		return 0, fmt.Errorf("sqlite: write blob: %w", err)
	}
	fullLen := len(s)
	for len(s) > 0 {
		nn := copy(blob.bufSlice(), s)
		res := ResultCode(lib.Xsqlite3_blob_write(blob.conn.tls, blob.blob, blob.buf, int32(nn), blob.off))
		if err := res.ToError(); err != nil {
			return fullLen - len(s), fmt.Errorf("sqlite: write blob: %w", err)
		}
		s = s[nn:]
		blob.off += int32(nn)
	}
	return fullLen, nil
}

// ReadFrom copies data from r to the blob until EOF or error.
func (blob *Blob) ReadFrom(r io.Reader) (n int64, err error) {
	if blob.blob == 0 {
		return 0, fmt.Errorf("sqlite: write blob: %w", errInvalidBlob)
	}
	if err := blob.conn.interrupted(); err != nil {
		return 0, fmt.Errorf("sqlite: write blob: %w", err)
	}
	for {
		nn, err := r.Read(blob.bufSlice())
		if nn > 0 {
			res := ResultCode(lib.Xsqlite3_blob_write(blob.conn.tls, blob.blob, blob.buf, int32(nn), blob.off))
			if err := res.ToError(); err != nil {
				return n, fmt.Errorf("sqlite: write blob: %w", err)
			}
			n += int64(nn)
			blob.off += int32(nn)
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return n, err
		}
	}
}

// Seek sets the offset for the next Read or Write and returns the offset.
// Seeking past the end of the blob returns an error.
func (blob *Blob) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		// use offset directly
	case io.SeekCurrent:
		offset += int64(blob.off)
	case io.SeekEnd:
		offset += int64(blob.size)
	default:
		return int64(blob.off), fmt.Errorf("sqlite: seek blob: invalid whence %d", whence)
	}
	if offset < 0 {
		return int64(blob.off), fmt.Errorf("sqlite: seek blob: negative offset %d", offset)
	}
	if offset > int64(blob.size) {
		return int64(blob.off), fmt.Errorf("sqlite: seek blob: offset %d is past size %d", offset, blob.size)
	}
	blob.off = int32(offset)
	return offset, nil
}

// Size returns the number of bytes in the blob.
func (blob *Blob) Size() int64 {
	return int64(blob.size)
}

// Close releases any resources associated with the blob handle.
// https://www.sqlite.org/c3ref/blob_close.html
func (blob *Blob) Close() error {
	if blob.blob == 0 {
		return errInvalidBlob
	}
	libc.Xfree(blob.conn.tls, blob.buf)
	blob.buf = 0
	res := ResultCode(lib.Xsqlite3_blob_close(blob.conn.tls, blob.blob))
	blob.blob = 0
	if err := res.ToError(); err != nil {
		return fmt.Errorf("sqlite: close blob: %w", err)
	}
	return nil
}

var errInvalidBlob = errors.New("invalid blob")

// cDBName converts a database name into a C string.
func cDBName(dbn string) (uintptr, func(), error) {
	switch dbn {
	case "", "main":
		return mainCString, func() {}, nil
	case "temp":
		return tempCString, func() {}, nil
	default:
		cdb, err := libc.CString(dbn)
		if err != nil {
			return 0, nil, err
		}
		return cdb, func() { libc.Xfree(nil, cdb) }, nil
	}
}

// TODO: Blob Reopen
