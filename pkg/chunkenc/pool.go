package chunkenc

import (
	"bufio"
	"bytes"
	"io"
	"runtime"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/prometheus/prometheus/util/pool"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// WriterPool is a pool of io.Writer
// This is used by every chunk to avoid unnecessary allocations.
type WriterPool interface {
	GetWriter(io.Writer) io.WriteCloser
	PutWriter(io.WriteCloser)
}

// ReaderPool similar to WriterPool but for reading chunks.
type ReaderPool interface {
	GetReader(io.Reader) (io.Reader, error)
	PutReader(io.Reader)
}

var (
	// Gzip is the gnu zip compression pool
	Gzip     = GzipPool{level: gzip.DefaultCompression}
	Lz4_64k  = LZ4Pool{bufferSize: 1 << 16} // Lz4_64k is the l4z compression pool, with 64k buffer size
	Lz4_256k = LZ4Pool{bufferSize: 1 << 18} // Lz4_256k uses 256k buffer
	Lz4_1M   = LZ4Pool{bufferSize: 1 << 20} // Lz4_1M uses 1M buffer
	Lz4_4M   = LZ4Pool{bufferSize: 1 << 22} // Lz4_4M uses 4M buffer
	Flate    = FlatePool{}
	Zstd     = ZstdPool{}
	// Snappy is the snappy compression pool
	Snappy SnappyPool
	// Noop is the no compression pool
	Noop NoopPool

	// BytesBufferPool is a bytes buffer used for lines decompressed.
	// Buckets [0.5KB,1KB,2KB,4KB,8KB]
	BytesBufferPool = pool.New(1<<9, 1<<13, 2, func(size int) interface{} { return make([]byte, 0, size) })

	// LabelsPool is a matrix of bytes buffers used to store label names and values.
	// Buckets [8, 16, 32, 64, 128, 256].
	// Since we store label names and values, the number of labels we can store is the half the bucket size.
	// So we will be able to store from 0 to 128 labels.
	LabelsPool = pool.New(1<<3, 1<<8, 2, func(size int) interface{} { return make([][]byte, 0, size) })

	SymbolsPool = pool.New(1<<3, 1<<8, 2, func(size int) interface{} { return make([]symbol, 0, size) })

	// SamplesPool pooling array of samples [512,1024,...,16k]
	SamplesPool = pool.New(1<<9, 1<<14, 2, func(size int) interface{} { return make([]logproto.Sample, 0, size) })

	// Pool of crc32 hash
	crc32HashPool = sync.Pool{
		New: func() interface{} {
			return newCRC32()
		},
	}

	serializeBytesBufferPool = sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}

	// EncodeBufferPool is a pool used to binary encode.
	EncodeBufferPool = sync.Pool{
		New: func() interface{} {
			return &encbuf{
				b: make([]byte, 0, 256),
			}
		},
	}
)

func GetWriterPool(enc Encoding) WriterPool {
	return GetReaderPool(enc).(WriterPool)
}

func GetReaderPool(enc Encoding) ReaderPool {
	switch enc {
	case EncGZIP:
		return &Gzip
	case EncLZ4_64k:
		return &Lz4_64k
	case EncLZ4_256k:
		return &Lz4_256k
	case EncLZ4_1M:
		return &Lz4_1M
	case EncLZ4_4M:
		return &Lz4_4M
	case EncSnappy:
		return &Snappy
	case EncNone:
		return &Noop
	case EncFlate:
		return &Flate
	case EncZstd:
		return &Zstd
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

// Gzip needs buffering to read efficiently.
// We need to be able to see the underlying gzip.Reader to Reset it.
type gzipBufferedReader struct {
	*bufio.Reader
	gzipReader *gzip.Reader
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *GzipPool) GetReader(src io.Reader) (io.Reader, error) {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*gzipBufferedReader)
		err := reader.gzipReader.Reset(src)
		if err != nil {
			return nil, err
		}
		reader.Reader.Reset(reader.gzipReader)
		return reader, nil
	}
	gzipReader, err := gzip.NewReader(src)
	if err != nil {
		return nil, err
	}
	return &gzipBufferedReader{
		gzipReader: gzipReader,
		Reader:     bufio.NewReaderSize(gzipReader, 4*1024),
	}, nil
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

// FlatePool is a flate compression pool
type FlatePool struct {
	readers sync.Pool
	writers sync.Pool
	level   int
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *FlatePool) GetReader(src io.Reader) (io.Reader, error) {
	if r := pool.readers.Get(); r != nil {
		reader := r.(flate.Resetter)
		err := reader.Reset(src, nil)
		if err != nil {
			panic(err)
		}
		return reader.(io.Reader), nil
	}
	return flate.NewReader(src), nil
}

// PutReader places back in the pool a CompressionReader
func (pool *FlatePool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *FlatePool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*flate.Writer)
		writer.Reset(dst)
		return writer
	}

	level := pool.level
	if level == 0 {
		level = flate.DefaultCompression
	}
	w, err := flate.NewWriter(dst, level)
	if err != nil {
		panic(err) // never happens, error is only returned on wrong compression level.
	}
	return w
}

// PutWriter places back in the pool a CompressionWriter
func (pool *FlatePool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

// GzipPool is a gun zip compression pool
type ZstdPool struct {
	readers sync.Pool
	writers sync.Pool
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *ZstdPool) GetReader(src io.Reader) (io.Reader, error) {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*zstd.Decoder)
		err := reader.Reset(src)
		if err != nil {
			return nil, err
		}
		return reader, nil
	}
	reader, err := zstd.NewReader(src)
	if err != nil {
		return nil, err
	}
	runtime.SetFinalizer(reader, (*zstd.Decoder).Close)
	return reader, nil
}

// PutReader places back in the pool a CompressionReader
func (pool *ZstdPool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *ZstdPool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*zstd.Encoder)
		writer.Reset(dst)
		return writer
	}

	w, err := zstd.NewWriter(dst)
	if err != nil {
		panic(err) // never happens, error is only returned on wrong compression level.
	}
	return w
}

// PutWriter places back in the pool a CompressionWriter
func (pool *ZstdPool) PutWriter(writer io.WriteCloser) {
	pool.writers.Put(writer)
}

type LZ4Pool struct {
	readers    sync.Pool
	writers    sync.Pool
	bufferSize uint32 // available values: 1<<16 (64k), 1<<18 (256k), 1<<20 (1M), 1<<22 (4M). Defaults to 4MB, if not set.
}

// We need to be able to see the underlying lz4.Reader to Reset it.
type lz4BufferedReader struct {
	*bufio.Reader
	lz4Reader *lz4.Reader
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *LZ4Pool) GetReader(src io.Reader) (io.Reader, error) {
	var r *lz4BufferedReader
	if pooled := pool.readers.Get(); pooled != nil {
		r = pooled.(*lz4BufferedReader)
		r.lz4Reader.Reset(src)
		r.Reader.Reset(r.lz4Reader)
	} else {
		lz4Reader := lz4.NewReader(src)
		r = &lz4BufferedReader{
			lz4Reader: lz4Reader,
			Reader:    bufio.NewReaderSize(lz4Reader, 4*1024),
		}
	}
	return r, nil
}

// PutReader places back in the pool a CompressionReader
func (pool *LZ4Pool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *LZ4Pool) GetWriter(dst io.Writer) io.WriteCloser {
	var w *lz4.Writer
	if fromPool := pool.writers.Get(); fromPool != nil {
		w = fromPool.(*lz4.Writer)
		w.Reset(dst)
	} else {
		w = lz4.NewWriter(dst)
	}
	err := w.Apply(
		lz4.ChecksumOption(false),
		lz4.BlockSizeOption(lz4.BlockSize(pool.bufferSize)),
		lz4.CompressionLevelOption(lz4.Fast),
	)
	if err != nil {
		panic(err)
	}
	return w
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
func (pool *SnappyPool) GetReader(src io.Reader) (io.Reader, error) {
	if r := pool.readers.Get(); r != nil {
		reader := r.(*snappy.Reader)
		reader.Reset(src)
		return reader, nil
	}
	return snappy.NewReader(src), nil
}

// PutReader places back in the pool a CompressionReader
func (pool *SnappyPool) PutReader(reader io.Reader) {
	r := reader.(*snappy.Reader)
	// Reset to free reference to the underlying reader
	r.Reset(nil)
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

type NoopPool struct{}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *NoopPool) GetReader(src io.Reader) (io.Reader, error) {
	return src, nil
}

// PutReader places back in the pool a CompressionReader
func (pool *NoopPool) PutReader(_ io.Reader) {}

type noopCloser struct {
	io.Writer
}

func (noopCloser) Close() error { return nil }

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *NoopPool) GetWriter(dst io.Writer) io.WriteCloser {
	return noopCloser{dst}
}

// PutWriter places back in the pool a CompressionWriter
func (pool *NoopPool) PutWriter(_ io.WriteCloser) {}
