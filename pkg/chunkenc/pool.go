package chunkenc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"sync"
)

type CompressionPool interface {
	GetWriter(io.Writer) CompressionWriter
	PutWriter(CompressionWriter)
	GetReader(io.Reader) CompressionReader
	PutReader(CompressionReader)
}

var (
	Gzip          GzipPool
	BufReaderPool = &BufioReaderPool{
		pool: sync.Pool{
			New: func() interface{} { return bufio.NewReader(nil) },
		},
	}
	ByteBufferPool = sync.Pool{
		// New is called when a new instance is needed
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	LineBufferPool   = newBufferPoolWithSize(1024)
	IntBinBufferPool = newBufferPoolWithSize(binary.MaxVarintLen64)
)

type GzipPool struct {
	readers sync.Pool
	writers sync.Pool
}

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

func (pool *GzipPool) PutReader(reader CompressionReader) {
	pool.readers.Put(reader)
}

func (pool *GzipPool) GetWriter(dst io.Writer) (writer CompressionWriter) {
	if w := pool.writers.Get(); w != nil {
		writer = w.(CompressionWriter)
		writer.Reset(dst)
	} else {
		writer = gzip.NewWriter(dst)
	}
	return writer
}

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
	b.Reset(nil)
	bufPool.pool.Put(b)
}

type bufferPool struct {
	pool sync.Pool
}

func newBufferPoolWithSize(size int) *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} { return make([]byte, size) },
		},
	}
}

func (bp *bufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

func (bp *bufferPool) Put(b []byte) {
	bp.pool.Put(b)
}
