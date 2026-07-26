// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/util/filepool/filepool.go
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Mimir Authors.

package index

import (
	"errors"
	"os"
	"sync"

	"go.uber.org/atomic"
)

// DefaultMaxIdleFileHandles is the number of open file handles the filePool
// keeps around for reuse per index file. It trades a small number of open file
// descriptors per index file for a significant reduction in open(2)/close(2)
// syscalls on the query hot path.
const DefaultMaxIdleFileHandles = 4

// MaxIdleFileHandles controls how many idle file handles each pool-backed index
// reader keeps open for reuse. It can be tuned before opening index readers.
var MaxIdleFileHandles = DefaultMaxIdleFileHandles

// errPoolStopped is returned when a file handle is requested from a stopped pool.
var errPoolStopped = errors.New("index file handle pool is stopped")

// filePool maintains a bounded set of reusable file handles for a single file.
//
// It replaces memory-mapping the index file: instead of relying on the kernel to
// page the file into the process address space (which makes memory accounting
// unpredictable and can stall goroutines on page faults), reads are served from
// a small pool of regular file handles using pread(2) (via *os.File.ReadAt).
//
// Get and Put never block. If no idle handle is available, Get opens a new one;
// if the pool is full, Put closes the handle instead of retaining it. This mirrors
// the behaviour of Mimir's filepool.FilePool.
type filePool struct {
	path    string
	handles chan *os.File

	mtx     sync.RWMutex
	stopped bool
}

// newFilePool creates a file handle pool for path that keeps up to capacity idle
// handles around for reuse. A capacity of 0 means every Get opens a new handle
// and every Put closes it immediately.
func newFilePool(path string, capacity int) *filePool {
	if capacity < 0 {
		capacity = 0
	}
	return &filePool{
		path:    path,
		handles: make(chan *os.File, capacity),
	}
}

// get returns an idle file handle if one is available or opens a new one.
func (p *filePool) get() (*os.File, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	if p.stopped {
		return nil, errPoolStopped
	}

	select {
	case f := <-p.handles:
		return f, nil
	default:
		return os.Open(p.path)
	}
}

// put returns a file handle to the pool, or closes it if the pool is full or
// stopped.
func (p *filePool) put(f *os.File) error {
	if f == nil {
		return nil
	}

	p.mtx.RLock()
	defer p.mtx.RUnlock()

	if p.stopped {
		return f.Close()
	}

	select {
	case p.handles <- f:
		return nil
	default:
		return f.Close()
	}
}

// stop closes all idle handles. Subsequent Get calls return an error and Put
// calls close handles immediately.
func (p *filePool) stop() error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.stopped {
		return nil
	}
	p.stopped = true

	var err error
	for {
		select {
		case f := <-p.handles:
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
		default:
			return err
		}
	}
}

// scratchBufPool holds transient read buffers used for section reads whose
// contents are not retained by the caller (e.g. symbol lookups). This is the
// buffer-pool analogue of Mimir's bufio.Reader pool: it keeps read buffers off
// the heap-allocation hot path.
var scratchBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

// poolByteSlice implements ByteSlice on top of a filePool. It is the replacement
// for mmap-backed RealByteSlice used when opening an index file from disk.
//
// Range performs a single pread into a freshly allocated buffer. Fresh
// allocations are required because callers (for example the postings decoder)
// retain the returned slice; a pooled buffer could be recycled while still
// referenced. For reads whose result is not retained, use readRange, which
// serves the read from scratchBufPool.
type poolByteSlice struct {
	pool   *filePool
	length int

	// err records the first I/O error observed while reading. Because the
	// ByteSlice interface cannot return an error from Range, callers on the hot
	// path check Err after decoding sections that are not CRC-protected.
	err atomic.Error
}

func newPoolByteSlice(path string, length, maxIdleHandles int) *poolByteSlice {
	return &poolByteSlice{
		pool:   newFilePool(path, maxIdleHandles),
		length: length,
	}
}

func (b *poolByteSlice) Len() int { return b.length }

func (b *poolByteSlice) Range(start, end int) []byte {
	buf := make([]byte, end-start)
	if err := b.readAt(buf, start); err != nil {
		b.setErr(err)
	}
	return buf
}

// readRange reads bytes [start, end) into a buffer obtained from scratchBufPool
// and returns it together with a release function. The returned slice MUST NOT
// be retained after release is called.
func (b *poolByteSlice) readRange(start, end int) ([]byte, func(), error) {
	n := end - start
	pb := scratchBufPool.Get().(*[]byte)
	if cap(*pb) < n {
		*pb = make([]byte, n)
	} else {
		*pb = (*pb)[:n]
	}
	buf := *pb
	if err := b.readAt(buf, start); err != nil {
		scratchBufPool.Put(pb)
		b.setErr(err)
		return nil, nil, err
	}
	return buf, func() { scratchBufPool.Put(pb) }, nil
}

func (b *poolByteSlice) readAt(buf []byte, start int) error {
	f, err := b.pool.get()
	if err != nil {
		return err
	}
	n, rerr := f.ReadAt(buf, int64(start))
	if perr := b.pool.put(f); perr != nil && rerr == nil {
		rerr = perr
	}
	// A read that fills the whole buffer is a success even if ReadAt reports
	// io.EOF, which it may when the read ends exactly at the end of the file
	// (see io.ReaderAt). A genuine short read (n < len(buf)) is returned as an
	// error.
	if n == len(buf) {
		return nil
	}
	return rerr
}

func (b *poolByteSlice) setErr(err error) {
	if err == nil {
		return
	}
	b.err.CompareAndSwap(nil, err)
}

// Err returns the first I/O error observed while reading, if any.
func (b *poolByteSlice) Err() error {
	return b.err.Load()
}

// Close releases all pooled file handles.
func (b *poolByteSlice) Close() error {
	return b.pool.stop()
}

// rangeReader is implemented by ByteSlice values that can serve a read into a
// pooled scratch buffer, avoiding an allocation when the result is not retained.
type rangeReader interface {
	readRange(start, end int) (buf []byte, release func(), err error)
}

// byteSliceErr returns the sticky read error of bs, if it tracks one.
func byteSliceErr(bs ByteSlice) error {
	if e, ok := bs.(interface{ Err() error }); ok {
		return e.Err()
	}
	return nil
}
