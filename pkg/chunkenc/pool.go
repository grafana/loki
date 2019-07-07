package chunkenc

import (
	"bufio"
	"bytes"
	"compress/gzip"

	"io"
	"sync"
)

// CompressionPool is a pool of CompressionWriter and CompressionReader
// This is used by every chunk to avoid unnecessary allocations.
type CompressionPool interface {
	GetWriter(io.Writer) CompressionWriter
	PutWriter(CompressionWriter)
	GetReader(io.Reader) CompressionReader
	PutReader(CompressionReader)
}

var (
	// Gzip is the gun zip compression pool
	Gzip GzipPool
	// BufReaderPool is bufio.Reader pool
	BufReaderPool = &BufioReaderPool{
		pool: sync.Pool{
			New: func() interface{} { return bufio.NewReader(nil) },
		},
	}
	// BytesBufferPool is a bytes buffer used for lines decompressed.
	BytesBufferPool = newBufferPoolWithSize(4096)
)

// GzipPool is a gun zip compression pool
type GzipPool struct {
	readers sync.Pool
	writers sync.Pool
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *GzipPool) GetReader(src io.Reader) (reader CompressionReader) {
	if r := pool.readers.Get(); r != nil {
		reader = r.(CompressionReader)
		err := reader.Reset(src)
		if err != nil {
			panic(err)
		}
	} else {
		var err error
		reader, err = gzip.NewReader(src)
		if err != nil {
			panic(err)
		}
	}
	return reader
}

// PutReader places back in the pool a CompressionReader
func (pool *GzipPool) PutReader(reader CompressionReader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *GzipPool) GetWriter(dst io.Writer) (writer CompressionWriter) {
	if w := pool.writers.Get(); w != nil {
		writer = w.(CompressionWriter)
		writer.Reset(dst)
	} else {
		writer = gzip.NewWriter(dst)
	}
	return writer
}

// PutWriter places back in the pool a CompressionWriter
func (pool *GzipPool) PutWriter(writer CompressionWriter) {
	pool.writers.Put(writer)
}

// BufioReaderPool is a bufio reader that uses sync.Pool.
type BufioReaderPool struct {
	pool sync.Pool
}

// Get returns a bufio.Reader which reads from r. The buffer size is that of the pool.
func (bufPool *BufioReaderPool) Get(r io.Reader) *bufio.Reader {
	buf := bufPool.pool.Get().(*bufio.Reader)
	buf.Reset(r)
	return buf
}

// Put puts the bufio.Reader back into the pool.
func (bufPool *BufioReaderPool) Put(b *bufio.Reader) {
	bufPool.pool.Put(b)
}

type bufferPool struct {
	pool sync.Pool
}

func newBufferPoolWithSize(size int) *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} { return bytes.NewBuffer(make([]byte, size)) },
		},
	}
}

func (bp *bufferPool) Get() *bytes.Buffer {
	return bp.pool.Get().(*bytes.Buffer)
}

func (bp *bufferPool) Put(b *bytes.Buffer) {
	bp.pool.Put(b)
}
