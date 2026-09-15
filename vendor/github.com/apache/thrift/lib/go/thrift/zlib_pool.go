/*
* Licensed to the Apache Software Foundation (ASF) under one
* or more contributor license agreements. See the NOTICE file
* distributed with this work for additional information
* regarding copyright ownership. The ASF licenses this file
* to you under the Apache License, Version 2.0 (the
* "License"); you may not use this file except in compliance
* with the License. You may obtain a copy of the License at
*
*   http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied. See the License for the
* specific language governing permissions and limitations
* under the License.
 */

package thrift

import (
	"compress/zlib"
	"io"
	"sync"
)

type zlibReader interface {
	io.ReadCloser
	zlib.Resetter
}

var zlibReaderPool sync.Pool

func newZlibReader(r io.Reader) (io.ReadCloser, error) {
	if reader, _ := zlibReaderPool.Get().(*wrappedZlibReader); reader != nil {
		if err := reader.Reset(r, nil); err == nil {
			return reader, nil
		}
	}
	reader, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &wrappedZlibReader{reader.(zlibReader)}, nil
}

type wrappedZlibReader struct {
	zlibReader
}

func (wr *wrappedZlibReader) Close() error {
	defer func() {
		zlibReaderPool.Put(wr)
	}()
	return wr.zlibReader.Close()
}

func newZlibWriterLevelMust(level int) *zlib.Writer {
	w, err := zlib.NewWriterLevel(nil, level)
	if err != nil {
		panic(err)
	}
	return w
}

// level -> pool
var zlibWriterPools map[int]*pool[zlib.Writer] = func() map[int]*pool[zlib.Writer] {
	m := make(map[int]*pool[zlib.Writer])
	for level := zlib.HuffmanOnly; level <= zlib.BestCompression; level++ {
		// force a panic at init if we have an invalid level here
		newZlibWriterLevelMust(level)
		m[level] = newPool(
			func() *zlib.Writer {
				return newZlibWriterLevelMust(level)
			},
			nil,
		)
	}
	return m
}()

type zlibWriterPoolCloser struct {
	writer *zlib.Writer
	pool   *pool[zlib.Writer]
}

func (z *zlibWriterPoolCloser) Close() error {
	defer func() {
		z.writer.Reset(nil)
		z.pool.put(&z.writer)
	}()
	return z.writer.Close()
}

func newZlibWriterCloserLevel(w io.Writer, level int) (*zlib.Writer, io.Closer, error) {
	pool, ok := zlibWriterPools[level]
	if !ok {
		// not pooled
		writer, err := zlib.NewWriterLevel(w, level)
		if err != nil {
			return nil, nil, err
		}
		return writer, writer, nil
	}
	writer := pool.get()
	writer.Reset(w)
	return writer, &zlibWriterPoolCloser{writer: writer, pool: pool}, nil
}
