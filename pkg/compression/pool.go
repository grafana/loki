package compression

import (
	"bufio"
	"io"
	"runtime"
	"sync"

	snappylib "github.com/golang/snappy"
	flatelib "github.com/klauspost/compress/flate"
	gziplib "github.com/klauspost/compress/gzip"
	zstdlib "github.com/klauspost/compress/zstd"
	lz4lib "github.com/pierrec/lz4/v4"
)

// WriterPool is a pool of io.Writer
// This is used by every chunk to avoid unnecessary allocations.
type WriterPool interface {
	GetWriter(io.Writer) io.WriteCloser
	PutWriter(io.WriteCloser)
}

// ReaderPool is a pool of io.Reader
// ReaderPool similar to WriterPool but for reading chunks.
type ReaderPool interface {
	GetReader(io.Reader) (io.Reader, error)
	PutReader(io.Reader)
}

// ReaderPool is a pool of io.Reader and io.Writer
type ReaderWriterPool interface {
	ReaderPool
	WriterPool
}

var (
	// gzip is the gnu zip compression pool
	gzip = GzipPool{level: gziplib.DefaultCompression}
	// lz4_* are the lz4 compression pools
	lz4_64k  = LZ4Pool{bufferSize: 1 << 16} // lz4_64k is the l4z compression pool, with 64k buffer size
	lz4_256k = LZ4Pool{bufferSize: 1 << 18} // lz4_256k uses 256k buffer
	lz4_1M   = LZ4Pool{bufferSize: 1 << 20} // lz4_1M uses 1M buffer
	lz4_4M   = LZ4Pool{bufferSize: 1 << 22} // lz4_4M uses 4M buffer
	// flate is the flate compression pool
	flate = FlatePool{}
	// zstd is the zstd compression pool
	zstd = ZstdPool{}
	// snappy is the snappy compression pool
	snappy = SnappyPool{}
	// noop is the no compression pool
	noop = NoopPool{}
)

func GetWriterPool(enc Codec) WriterPool {
	return GetPool(enc).(WriterPool)
}

func GetReaderPool(enc Codec) ReaderPool {
	return GetPool(enc).(ReaderPool)
}

func GetPool(enc Codec) ReaderWriterPool {
	switch enc {
	case GZIP:
		return &gzip
	case LZ4_64k:
		return &lz4_64k
	case LZ4_256k:
		return &lz4_256k
	case LZ4_1M:
		return &lz4_1M
	case LZ4_4M:
		return &lz4_4M
	case Snappy:
		return &snappy
	case None:
		return &noop
	case Flate:
		return &flate
	case Zstd:
		return &zstd
	default:
		panic("unknown encoding")
	}
}

// GzipPool is a gnu zip compression pool
type GzipPool struct {
	readers sync.Pool
	writers sync.Pool
	level   int
}

// Gzip needs buffering to read efficiently.
// We need to be able to see the underlying gzip.Reader to Reset it.
type gzipBufferedReader struct {
	*bufio.Reader
	gzipReader *gziplib.Reader
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
	gzipReader, err := gziplib.NewReader(src)
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
		writer := w.(*gziplib.Writer)
		writer.Reset(dst)
		return writer
	}

	level := pool.level
	if level == 0 {
		level = gziplib.DefaultCompression
	}
	w, err := gziplib.NewWriterLevel(dst, level)
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
		reader := r.(flatelib.Resetter)
		err := reader.Reset(src, nil)
		if err != nil {
			panic(err)
		}
		return reader.(io.Reader), nil
	}
	return flatelib.NewReader(src), nil
}

// PutReader places back in the pool a CompressionReader
func (pool *FlatePool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *FlatePool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*flatelib.Writer)
		writer.Reset(dst)
		return writer
	}

	level := pool.level
	if level == 0 {
		level = flatelib.DefaultCompression
	}
	w, err := flatelib.NewWriter(dst, level)
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
		reader := r.(*zstdlib.Decoder)
		err := reader.Reset(src)
		if err != nil {
			return nil, err
		}
		return reader, nil
	}
	reader, err := zstdlib.NewReader(src)
	if err != nil {
		return nil, err
	}
	runtime.SetFinalizer(reader, (*zstdlib.Decoder).Close)
	return reader, nil
}

// PutReader places back in the pool a CompressionReader
func (pool *ZstdPool) PutReader(reader io.Reader) {
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *ZstdPool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*zstdlib.Encoder)
		writer.Reset(dst)
		return writer
	}

	w, err := zstdlib.NewWriter(dst)
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
	lz4Reader *lz4lib.Reader
}

// GetReader gets or creates a new CompressionReader and reset it to read from src
func (pool *LZ4Pool) GetReader(src io.Reader) (io.Reader, error) {
	var r *lz4BufferedReader
	if pooled := pool.readers.Get(); pooled != nil {
		r = pooled.(*lz4BufferedReader)
		r.lz4Reader.Reset(src)
		r.Reader.Reset(r.lz4Reader)
	} else {
		lz4Reader := lz4lib.NewReader(src)
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
	var w *lz4lib.Writer
	if fromPool := pool.writers.Get(); fromPool != nil {
		w = fromPool.(*lz4lib.Writer)
		w.Reset(dst)
	} else {
		w = lz4lib.NewWriter(dst)
	}
	err := w.Apply(
		lz4lib.ChecksumOption(false),
		lz4lib.BlockSizeOption(lz4lib.BlockSize(pool.bufferSize)),
		lz4lib.CompressionLevelOption(lz4lib.Fast),
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
		reader := r.(*snappylib.Reader)
		reader.Reset(src)
		return reader, nil
	}
	return snappylib.NewReader(src), nil
}

// PutReader places back in the pool a CompressionReader
func (pool *SnappyPool) PutReader(reader io.Reader) {
	r := reader.(*snappylib.Reader)
	// Reset to free reference to the underlying reader
	r.Reset(nil)
	pool.readers.Put(reader)
}

// GetWriter gets or creates a new CompressionWriter and reset it to write to dst
func (pool *SnappyPool) GetWriter(dst io.Writer) io.WriteCloser {
	if w := pool.writers.Get(); w != nil {
		writer := w.(*snappylib.Writer)
		writer.Reset(dst)
		return writer
	}
	return snappylib.NewBufferedWriter(dst)
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
