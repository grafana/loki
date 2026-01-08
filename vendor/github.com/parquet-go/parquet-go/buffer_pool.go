package parquet

import (
	"io"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go/internal/memory"
)

// BufferPool is an interface abstracting the underlying implementation of
// page buffer pools.
//
// The parquet-go package provides two implementations of this interface, one
// backed by in-memory buffers (on the Go heap), and the other using temporary
// files on disk.
//
// Applications which need finer grain control over the allocation and retention
// of page buffers may choose to provide their own implementation and install it
// via the parquet.ColumnPageBuffers writer option.
//
// BufferPool implementations must be safe to use concurrently from multiple
// goroutines.
type BufferPool interface {
	// GetBuffer is called when a parquet writer needs to acquire a new
	// page buffer from the pool.
	GetBuffer() io.ReadWriteSeeker

	// PutBuffer is called when a parquet writer releases a page buffer to
	// the pool.
	//
	// The parquet.Writer type guarantees that the buffers it calls this method
	// with were previously acquired by a call to GetBuffer on the same
	// pool, and that it will not use them anymore after the call.
	PutBuffer(io.ReadWriteSeeker)
}

const defaultChunkSize = 32 * 1024 // 32 KiB

// NewBufferPool creates a new in-memory page buffer pool.
//
// The implementation is backed by sync.Pool and allocates memory buffers on the
// Go heap.
func NewBufferPool() BufferPool { return NewChunkBufferPool(defaultChunkSize) }

// NewChunkBufferPool creates a new in-memory page buffer pool.
//
// The implementation is backed by sync.Pool and allocates memory buffers on the
// Go heap in fixed-size chunks.
func NewChunkBufferPool(chunkSize int) BufferPool {
	return &memoryBufferPool{size: chunkSize}
}

type memoryBufferPool struct {
	pool memory.Pool[memory.Buffer]
	size int
}

func (m *memoryBufferPool) GetBuffer() io.ReadWriteSeeker {
	return m.pool.Get(
		func() *memory.Buffer { return memory.NewBuffer(m.size) },
		(*memory.Buffer).Reset,
	)
}

func (m *memoryBufferPool) PutBuffer(buf io.ReadWriteSeeker) {
	if b, _ := buf.(*memory.Buffer); b != nil {
		b.Reset() // release internal buffers
		m.pool.Put(b)
	}
}

type fileBufferPool struct {
	err     error
	tempdir string
	pattern string
}

// NewFileBufferPool creates a new on-disk page buffer pool.
func NewFileBufferPool(tempdir, pattern string) BufferPool {
	pool := &fileBufferPool{
		tempdir: tempdir,
		pattern: pattern,
	}
	pool.tempdir, pool.err = filepath.Abs(pool.tempdir)
	return pool
}

func (pool *fileBufferPool) GetBuffer() io.ReadWriteSeeker {
	if pool.err != nil {
		return &errorBuffer{err: pool.err}
	}
	f, err := os.CreateTemp(pool.tempdir, pool.pattern)
	if err != nil {
		return &errorBuffer{err: err}
	}
	return f
}

func (pool *fileBufferPool) PutBuffer(buf io.ReadWriteSeeker) {
	if f, _ := buf.(*os.File); f != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
}

type errorBuffer struct{ err error }

func (buf *errorBuffer) Read([]byte) (int, error)          { return 0, buf.err }
func (buf *errorBuffer) Write([]byte) (int, error)         { return 0, buf.err }
func (buf *errorBuffer) ReadFrom(io.Reader) (int64, error) { return 0, buf.err }
func (buf *errorBuffer) WriteTo(io.Writer) (int64, error)  { return 0, buf.err }
func (buf *errorBuffer) Seek(int64, int) (int64, error)    { return 0, buf.err }

var (
	defaultColumnBufferPool  = memoryBufferPool{size: defaultChunkSize}
	defaultSortingBufferPool = memoryBufferPool{size: defaultChunkSize}

	_ io.ReaderFrom = (*errorBuffer)(nil)
	_ io.WriterTo   = (*errorBuffer)(nil)
)

type readerAt struct {
	reader io.ReadSeeker
	offset int64
}

func (r *readerAt) ReadAt(b []byte, off int64) (int, error) {
	if r.offset < 0 || off != r.offset {
		off, err := r.reader.Seek(off, io.SeekStart)
		if err != nil {
			return 0, err
		}
		r.offset = off
	}
	n, err := r.reader.Read(b)
	r.offset += int64(n)
	return n, err
}

func newReaderAt(r io.ReadSeeker) io.ReaderAt {
	if rr, ok := r.(io.ReaderAt); ok {
		return rr
	}
	return &readerAt{reader: r, offset: -1}
}
