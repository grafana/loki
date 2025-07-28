// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2013 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"io"
)

const defaultBufSize = 4096
const maxCachedBufSize = 256 * 1024

// readerFunc is a function that compatible with io.Reader.
// We use this function type instead of io.Reader because we want to
// just pass mc.readWithTimeout.
type readerFunc func([]byte) (int, error)

// A buffer which is used for both reading and writing.
// This is possible since communication on each connection is synchronous.
// In other words, we can't write and read simultaneously on the same connection.
// The buffer is similar to bufio.Reader / Writer but zero-copy-ish
// Also highly optimized for this particular use case.
type buffer struct {
	buf       []byte // read buffer.
	cachedBuf []byte // buffer that will be reused. len(cachedBuf) <= maxCachedBufSize.
}

// newBuffer allocates and returns a new buffer.
func newBuffer() buffer {
	return buffer{
		cachedBuf: make([]byte, defaultBufSize),
	}
}

// busy returns true if the read buffer is not empty.
func (b *buffer) busy() bool {
	return len(b.buf) > 0
}

// len returns how many bytes in the read buffer.
func (b *buffer) len() int {
	return len(b.buf)
}

// fill reads into the read buffer until at least _need_ bytes are in it.
func (b *buffer) fill(need int, r readerFunc) error {
	// we'll move the contents of the current buffer to dest before filling it.
	dest := b.cachedBuf

	// grow buffer if necessary to fit the whole packet.
	if need > len(dest) {
		// Round up to the next multiple of the default size
		dest = make([]byte, ((need/defaultBufSize)+1)*defaultBufSize)

		// if the allocated buffer is not too large, move it to backing storage
		// to prevent extra allocations on applications that perform large reads
		if len(dest) <= maxCachedBufSize {
			b.cachedBuf = dest
		}
	}

	// move the existing data to the start of the buffer.
	n := len(b.buf)
	copy(dest[:n], b.buf)

	for {
		nn, err := r(dest[n:])
		n += nn

		if err == nil && n < need {
			continue
		}

		b.buf = dest[:n]

		if err == io.EOF {
			if n < need {
				err = io.ErrUnexpectedEOF
			} else {
				err = nil
			}
		}
		return err
	}
}

// returns next N bytes from buffer.
// The returned slice is only guaranteed to be valid until the next read
func (b *buffer) readNext(need int) []byte {
	data := b.buf[:need:need]
	b.buf = b.buf[need:]
	return data
}

// takeBuffer returns a buffer with the requested size.
// If possible, a slice from the existing buffer is returned.
// Otherwise a bigger buffer is made.
// Only one buffer (total) can be used at a time.
func (b *buffer) takeBuffer(length int) ([]byte, error) {
	if b.busy() {
		return nil, ErrBusyBuffer
	}

	// test (cheap) general case first
	if length <= len(b.cachedBuf) {
		return b.cachedBuf[:length], nil
	}

	if length < maxCachedBufSize {
		b.cachedBuf = make([]byte, length)
		return b.cachedBuf, nil
	}

	// buffer is larger than we want to store.
	return make([]byte, length), nil
}

// takeSmallBuffer is shortcut which can be used if length is
// known to be smaller than defaultBufSize.
// Only one buffer (total) can be used at a time.
func (b *buffer) takeSmallBuffer(length int) ([]byte, error) {
	if b.busy() {
		return nil, ErrBusyBuffer
	}
	return b.cachedBuf[:length], nil
}

// takeCompleteBuffer returns the complete existing buffer.
// This can be used if the necessary buffer size is unknown.
// cap and len of the returned buffer will be equal.
// Only one buffer (total) can be used at a time.
func (b *buffer) takeCompleteBuffer() ([]byte, error) {
	if b.busy() {
		return nil, ErrBusyBuffer
	}
	return b.cachedBuf, nil
}

// store stores buf, an updated buffer, if its suitable to do so.
func (b *buffer) store(buf []byte) {
	if cap(buf) <= maxCachedBufSize && cap(buf) > cap(b.cachedBuf) {
		b.cachedBuf = buf[:cap(buf)]
	}
}
