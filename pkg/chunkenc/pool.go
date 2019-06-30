package chunkenc

import (
	"bufio"
	"compress/gzip"

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
	bufPool.pool.Put(b)
}
