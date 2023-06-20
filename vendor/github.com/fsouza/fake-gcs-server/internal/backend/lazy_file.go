// Copyright 2022 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"io"
	"os"
	"sync"
)

type lazyReader struct {
	filename string
	once     *sync.Once
	f        *os.File
	err      error
}

func newLazyReader(filename string) io.ReadSeekCloser {
	return &lazyReader{
		filename: filename,
		once:     &sync.Once{},
	}
}

func (r *lazyReader) open() {
	r.f, r.err = os.Open(r.filename)
}

func (r *lazyReader) Read(p []byte) (int, error) {
	r.once.Do(r.open)
	if r.err != nil {
		return 0, r.err
	}
	return r.f.Read(p)
}

func (r *lazyReader) Seek(offset int64, whence int) (int64, error) {
	r.once.Do(r.open)
	if r.err != nil {
		return 0, r.err
	}
	return r.f.Seek(offset, whence)
}

func (r *lazyReader) Close() error {
	r.once.Do(r.open)
	if r.err != nil {
		return r.err
	}
	return r.f.Close()
}
