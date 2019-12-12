package chunkenc

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/s2"
	"github.com/pierrec/lz4"
	"github.com/prometheus/prometheus/pkg/pool"
)

// WriterPool is a pool of io.Writer
// This is used by every chunk to avoid unnecessary allocations.
type WriterPool interface {
	GetWriter(io.Writer) io.WriteCloser
	PutWriter(io.WriteCloser)
}

// ReaderPool similar to WriterPool but for reading chunks.
type ReaderPool interface {
	GetReader(io.Reader) io.Reader
	PutReader(io.Reader)
}

var (
	// Gzip is the gun zip compression pool
	Gzip = GzipPool{level: gzip.DefaultCompression}
	// GzipBestSpeed is the gun zip compression pool with best speed configuration
	GzipBestSpeed = GzipPool{level: gzip.BestSpeed}
	// LZ4 is the l4z compression pool
	LZ4 LZ4Pool
	// Snappy is the snappy compression pool
	Snappy SnappyPool
	// SnappyV2 is the snappy v2 compression pool
	SnappyV2 SnappyV2Pool
	// Noop is the no compression pool
	Noop NoopPool

	// BufReaderPool is bufio.Reader pool
	BufReaderPool = &BufioReaderPool{
		pool: sync.Pool{
			New: func() interface{} { return bufio.NewReader(nil) },
		},
	}
	// BytesBufferPool is a bytes buffer used for lines decompressed.
	// Buckets [0.5KB,1KB,2KB,4KB,8KB]
	BytesBufferPool          = pool.New(1<<9, 1<<13, 2, func(size int) interface{} { return make([]byte, 0, size) })
	serializeBytesBufferPool = sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}
)

func getWriterPool(enc Encoding) WriterPool {
	return getReaderPool(enc).(WriterPool)
}

func getReaderPool(enc Encoding) ReaderPool {
	switch enc {
	case EncGZIP:
		return &Gzip
	case EncGZIPBestSpeed:
		return &GzipBestSpeed
	case EncLZ4:
		return &LZ4
	case EncSnappy:
		return &Snappy
	case EncSnappyV2:
		return &SnappyV2
	case EncNone:
		return &Noop
	default:
		panic("unknown encoding")
	}
}

// GzipPool is a gun zip compression pool
type GzipPool struct {
	readers sync.Pool
	writers sync.Pool
	level   int
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *GzipPool) GetReader(src io.Reader) io.Reader {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*gzip.Reader)
		err := reader.Reset(src)
		if err != nil {
			panic(err)
		}
		return reader
	}
	reader, err := gzip.NewReader(src)
	if err != nil {
		panic(err)
	}
	return reader
}

// PutReader places back in the pool a CompressionReader
func (pool *GzipPool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *GzipPool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*gzip.Writer)
		writer.Reset(dst)
		return writer
	}

	level := pool.level
	if level == 0 {
		level = gzip.DefaultCompression
	}
	w, err := gzip.NewWriterLevel(dst, level)
	if err != nil {
		panic(err) // never happens, error is only returned on wrong compression level.
	}
	return w
}

// PutWriter places back in the pool a CompressionWriter
func (pool *GzipPool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

type LZ4Pool struct {
	readers sync.Pool
	writers sync.Pool
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *LZ4Pool) GetReader(src io.Reader) io.Reader {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*lz4.Reader)
		reader.Reset(src)
		return reader
	}
	return lz4.NewReader(src)
}

// PutReader places back in the pool a CompressionReader
func (pool *LZ4Pool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *LZ4Pool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*lz4.Writer)
		writer.Reset(dst)
		return writer
	}
	return lz4.NewWriter(dst)
}

// PutWriter places back in the pool a CompressionWriter
func (pool *LZ4Pool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

type SnappyPool struct {
	readers sync.Pool
	writers sync.Pool
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *SnappyPool) GetReader(src io.Reader) io.Reader {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*snappy.Reader)
		reader.Reset(src)
		return reader
	}
	return snappy.NewReader(src)
}

// PutReader places back in the pool a CompressionReader
func (pool *SnappyPool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *SnappyPool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*snappy.Writer)
		writer.Reset(dst)
		return writer
	}
	return snappy.NewBufferedWriter(dst)
}

// PutWriter places back in the pool a CompressionWriter
func (pool *SnappyPool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

type SnappyV2Pool struct {
	readers sync.Pool
	writers sync.Pool
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *SnappyV2Pool) GetReader(src io.Reader) io.Reader {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*s2.Reader)
		reader.Reset(src)
		return reader
	}
	return s2.NewReader(src)
}

// PutReader places back in the pool a CompressionReader
func (pool *SnappyV2Pool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *SnappyV2Pool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*s2.Writer)
		writer.Reset(dst)
		return writer
	}
	return s2.NewWriter(dst, s2.WriterBetterCompression())
}

// PutWriter places back in the pool a CompressionWriter
func (pool *SnappyV2Pool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

type NoopPool struct{}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *NoopPool) GetReader(src io.Reader) io.Reader {
	return src
}

// PutReader places back in the pool a CompressionReader
func (pool *NoopPool) PutReader(reader io.Reader) {}

type noopCloser struct {
	io.Writer
}

func (noopCloser) Close() error { return nil }

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *NoopPool) GetWriter(dst io.Writer) io.WriteCloser {
	return noopCloser{dst}
}

// PutWriter places back in the pool a CompressionWriter
func (pool *NoopPool) PutWriter(writer io.WriteCloser) {}

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
