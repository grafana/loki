package parquet

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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

// NewBufferPool creates a new in-memory page buffer pool.
//
// The implementation is backed by sync.Pool and allocates memory buffers on the
// Go heap.
func NewBufferPool() BufferPool { return new(memoryBufferPool) }

type memoryBuffer struct {
	data []byte
	off  int
}

func (p *memoryBuffer) Reset() {
	p.data, p.off = p.data[:0], 0
}

func (p *memoryBuffer) Read(b []byte) (n int, err error) {
	n = copy(b, p.data[p.off:])
	p.off += n
	if p.off == len(p.data) {
		err = io.EOF
	}
	return n, err
}

func (p *memoryBuffer) Write(b []byte) (int, error) {
	n := copy(p.data[p.off:cap(p.data)], b)
	p.data = p.data[:p.off+n]

	if n < len(b) {
		p.data = append(p.data, b[n:]...)
	}

	p.off += len(b)
	return len(b), nil
}

func (p *memoryBuffer) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(p.data[p.off:])
	p.off += n
	return int64(n), err
}

func (p *memoryBuffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += int64(p.off)
	case io.SeekEnd:
		offset += int64(len(p.data))
	}
	if offset < 0 {
		return 0, fmt.Errorf("seek: negative offset: %d<0", offset)
	}
	if offset > int64(len(p.data)) {
		offset = int64(len(p.data))
	}
	p.off = int(offset)
	return offset, nil
}

type memoryBufferPool struct{ sync.Pool }

func (pool *memoryBufferPool) GetBuffer() io.ReadWriteSeeker {
	b, _ := pool.Get().(*memoryBuffer)
	if b == nil {
		b = new(memoryBuffer)
	} else {
		b.Reset()
	}
	return b
}

func (pool *memoryBufferPool) PutBuffer(buf io.ReadWriteSeeker) {
	if b, _ := buf.(*memoryBuffer); b != nil {
		pool.Put(b)
	}
}

// NewChunkBufferPool creates a new in-memory page buffer pool.
//
// The implementation is backed by sync.Pool and allocates memory buffers on the
// Go heap in fixed-size chunks.
func NewChunkBufferPool(chunkSize int) BufferPool {
	return newChunkMemoryBufferPool(chunkSize)
}

func newChunkMemoryBufferPool(chunkSize int) *chunkMemoryBufferPool {
	pool := &chunkMemoryBufferPool{}
	pool.bytesPool.New = func() any {
		return make([]byte, chunkSize)
	}
	return pool
}

// chunkMemoryBuffer implements an io.ReadWriteSeeker by storing a slice of fixed-size
// buffers into which it copies data. (It uses a sync.Pool to reuse buffers across
// instances.)
type chunkMemoryBuffer struct {
	bytesPool *sync.Pool

	data [][]byte
	idx  int
	off  int
}

func (c *chunkMemoryBuffer) Reset() {
	for i := range c.data {
		c.bytesPool.Put(c.data[i])
	}
	for i := range c.data {
		c.data[i] = nil
	}
	c.data, c.idx, c.off = c.data[:0], 0, 0
}

func (c *chunkMemoryBuffer) Read(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}

	if c.idx >= len(c.data) {
		return 0, io.EOF
	}

	curData := c.data[c.idx]

	if c.idx == len(c.data)-1 && c.off == len(curData) {
		return 0, io.EOF
	}

	n = copy(b, curData[c.off:])
	c.off += n

	if c.off == cap(curData) {
		c.idx++
		c.off = 0
	}

	return n, err
}

func (c *chunkMemoryBuffer) Write(b []byte) (int, error) {
	lenB := len(b)

	if lenB == 0 {
		return 0, nil
	}

	for len(b) > 0 {
		if c.idx == len(c.data) {
			c.data = append(c.data, c.bytesPool.Get().([]byte)[:0])
		}
		curData := c.data[c.idx]
		n := copy(curData[c.off:cap(curData)], b)
		c.data[c.idx] = curData[:c.off+n]
		c.off += n
		b = b[n:]
		if c.off >= cap(curData) {
			c.idx++
			c.off = 0
		}
	}

	return lenB, nil
}

func (c *chunkMemoryBuffer) WriteTo(w io.Writer) (int64, error) {
	var numWritten int64
	var err error
	for err == nil {
		curData := c.data[c.idx]
		n, e := w.Write(curData[c.off:])
		numWritten += int64(n)
		err = e
		if c.idx == len(c.data)-1 {
			c.off = int(numWritten)
			break
		}
		c.idx++
		c.off = 0
	}
	return numWritten, err
}

func (c *chunkMemoryBuffer) Seek(offset int64, whence int) (int64, error) {
	// Because this is the common case, we check it first to avoid computing endOff.
	if offset == 0 && whence == io.SeekStart {
		c.idx = 0
		c.off = 0
		return offset, nil
	}
	endOff := c.endOff()
	switch whence {
	case io.SeekCurrent:
		offset += c.currentOff()
	case io.SeekEnd:
		offset += endOff
	}
	if offset < 0 {
		return 0, fmt.Errorf("seek: negative offset: %d<0", offset)
	}
	if offset > endOff {
		offset = endOff
	}
	// Repeat this case now that we know the absolute offset. This is a bit faster, but
	// mainly protects us from an out-of-bounds if c.data is empty. (If the buffer is
	// empty and the absolute offset isn't zero, we'd have errored (if negative) or
	// clamped to zero (if positive) above.
	if offset == 0 {
		c.idx = 0
		c.off = 0
	} else {
		stride := cap(c.data[0])
		c.idx = int(offset) / stride
		c.off = int(offset) % stride
	}
	return offset, nil
}

func (c *chunkMemoryBuffer) currentOff() int64 {
	if c.idx == 0 {
		return int64(c.off)
	}
	return int64((c.idx-1)*cap(c.data[0]) + c.off)
}

func (c *chunkMemoryBuffer) endOff() int64 {
	if len(c.data) == 0 {
		return 0
	}
	l := len(c.data)
	last := c.data[l-1]
	return int64(cap(last)*(l-1) + len(last))
}

type chunkMemoryBufferPool struct {
	sync.Pool
	bytesPool sync.Pool
}

func (pool *chunkMemoryBufferPool) GetBuffer() io.ReadWriteSeeker {
	b, _ := pool.Get().(*chunkMemoryBuffer)
	if b == nil {
		b = &chunkMemoryBuffer{bytesPool: &pool.bytesPool}
	} else {
		b.Reset()
	}
	return b
}

func (pool *chunkMemoryBufferPool) PutBuffer(buf io.ReadWriteSeeker) {
	if b, _ := buf.(*chunkMemoryBuffer); b != nil {
		for _, bytes := range b.data {
			b.bytesPool.Put(bytes)
		}
		for i := range b.data {
			b.data[i] = nil
		}
		b.data = b.data[:0]
		pool.Put(b)
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
		defer f.Close()
		os.Remove(f.Name())
	}
}

type errorBuffer struct{ err error }

func (buf *errorBuffer) Read([]byte) (int, error)          { return 0, buf.err }
func (buf *errorBuffer) Write([]byte) (int, error)         { return 0, buf.err }
func (buf *errorBuffer) ReadFrom(io.Reader) (int64, error) { return 0, buf.err }
func (buf *errorBuffer) WriteTo(io.Writer) (int64, error)  { return 0, buf.err }
func (buf *errorBuffer) Seek(int64, int) (int64, error)    { return 0, buf.err }

var (
	defaultColumnBufferPool  = *newChunkMemoryBufferPool(256 * 1024)
	defaultSortingBufferPool memoryBufferPool

	_ io.ReaderFrom      = (*errorBuffer)(nil)
	_ io.WriterTo        = (*errorBuffer)(nil)
	_ io.ReadWriteSeeker = (*memoryBuffer)(nil)
	_ io.WriterTo        = (*memoryBuffer)(nil)
	_ io.ReadWriteSeeker = (*chunkMemoryBuffer)(nil)
	_ io.WriterTo        = (*chunkMemoryBuffer)(nil)
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
