// This file implements a residency-aware random-access reader for index files.
//
// The design follows the approach described in
// https://internals-for-interns.com/posts/mmap-vs-pread-go-storage-engine/
// (based on the VictoriaMetrics filesystem layer):
//
//   - Prefer mmap when the requested pages are already resident in the page
//     cache. In that case a read is just a bounds check and a memory copy in
//     user space, avoiding a syscall per read.
//   - Fall back to pread (os.File.ReadAt) when the pages are not resident.
//     Touching a cold mmap page triggers a major page fault that blocks the
//     underlying OS thread invisibly to the Go scheduler. A blocking ReadAt, on
//     the other hand, is visible to the runtime, which can keep other goroutines
//     running while the thread waits for storage.
//
// Page residency is probed with mincore(2) and cached in a bitmap so the check
// stays cheap on the hot path. The cache is cleared periodically because the
// kernel may evict file-backed pages under memory pressure at any time.
//
// On platforms without mincore support, or on 32-bit architectures where large
// mappings are impractical, mmap is disabled and every read goes through pread.

package index

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// is32BitPtr reports whether the current architecture uses 32-bit pointers.
// Large files cannot reliably be mapped into a 32-bit address space, so mmap is
// disabled by default there.
const is32BitPtr = (^uintptr(0) >> 32) == 0

// mincoreCacheTTL is how long a cached page-residency bit is trusted before the
// residency cache is cleared and pages are probed again.
const mincoreCacheTTL = 60 * time.Second

// fileByteSlice is a ByteSlice backed by a file. It serves reads from an mmap
// region when the requested pages are resident, and falls back to positioned
// file reads (pread) otherwise.
type fileByteSlice struct {
	f    *os.File
	size int // logical file size

	// data is the mmap-ed region. It is nil when mmap is disabled or
	// unsupported, in which case every read goes through pread.
	data []byte

	// mincoreBits caches page residency. One bit per page; a set bit means the
	// page was recently observed resident. Words are updated atomically.
	mincoreBits        []atomic.Uint64
	mincoreNextCleanup atomic.Int64 // unix seconds

	pageSize int
}

// openFileByteSlice opens path for random access, mmap-ing it when supported.
func openFileByteSlice(path string) (bs *fileByteSlice, retErr error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index file: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = f.Close()
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat index file: %w", err)
	}
	size := info.Size()
	if int64(int(size)) != size {
		return nil, fmt.Errorf("index file is too big: %d bytes", size)
	}

	bs = &fileByteSlice{
		f:        f,
		size:     int(size),
		pageSize: os.Getpagesize(),
	}

	// Only reach for mmap on 64-bit platforms that support residency probing.
	if !is32BitPtr && hasMincore() {
		data, err := mmapFile(f, int(size))
		if err != nil {
			return nil, fmt.Errorf("mmap index file: %w", err)
		}
		bs.data = data
		pages := (bs.size + bs.pageSize - 1) / bs.pageSize
		words := (pages + 63) / 64
		bs.mincoreBits = make([]atomic.Uint64, words)
	}

	return bs, nil
}

func (b *fileByteSlice) Len() int {
	return b.size
}

func (b *fileByteSlice) Range(start, end int) []byte {
	if start < 0 || end > b.size || start > end {
		panic(fmt.Sprintf("index: invalid range [%d, %d) for size %d", start, end, b.size))
	}
	if start == end {
		return nil
	}

	// No mapping available: always go through pread.
	if len(b.data) == 0 {
		return b.readViaSyscall(start, end)
	}

	// Mapping available: use it only if the pages are known-resident, otherwise
	// avoid the potential major page fault and use pread instead.
	if b.canFastReadViaMmap(start, end) {
		return b.data[start:end:end]
	}
	return b.readViaSyscall(start, end)
}

func (b *fileByteSlice) Sub(start, end int) ByteSlice {
	return subByteSlice{bs: b, off: start, length: end - start}
}

// readViaSyscall reads [start, end) with a positioned file read into a fresh
// buffer. On success it marks the touched pages as resident so subsequent reads
// of the same range can take the fast mmap path.
func (b *fileByteSlice) readViaSyscall(start, end int) []byte {
	buf := make([]byte, end-start)
	n, err := b.f.ReadAt(buf, int64(start))
	if err != nil || n != len(buf) {
		panic(fmt.Sprintf("index: cannot read %d bytes at offset %d: %v", len(buf), start, err))
	}
	b.markResident(start, end)
	return buf
}

// canFastReadViaMmap reports whether every page covering [start, end) is
// resident, so it can be read from the mapping without risking a major page
// fault. Residency is cached in mincoreBits and refreshed via mincore(2).
func (b *fileByteSlice) canFastReadViaMmap(start, end int) bool {
	b.maybeCleanupCache()

	off := start - start%b.pageSize
	pageIdx := off / b.pageSize
	for off < end {
		wordIdx := pageIdx / 64
		bitIdx := uint(pageIdx % 64)
		mask := uint64(1) << bitIdx
		wordPtr := &b.mincoreBits[wordIdx]
		word := wordPtr.Load()
		if word&mask == 0 {
			if !mincore(&b.data[off], b.pageSize) {
				return false
			}
			for word&mask == 0 && !wordPtr.CompareAndSwap(word, word|mask) {
				word = wordPtr.Load()
			}
		}
		off += b.pageSize
		pageIdx++
	}
	return true
}

// markResident records that the pages covering [start, end) are resident.
func (b *fileByteSlice) markResident(start, end int) {
	if len(b.data) == 0 {
		return
	}
	off := start - start%b.pageSize
	pageIdx := off / b.pageSize
	for off < end {
		wordIdx := pageIdx / 64
		bitIdx := uint(pageIdx % 64)
		mask := uint64(1) << bitIdx
		wordPtr := &b.mincoreBits[wordIdx]
		word := wordPtr.Load()
		for word&mask == 0 && !wordPtr.CompareAndSwap(word, word|mask) {
			word = wordPtr.Load()
		}
		off += b.pageSize
		pageIdx++
	}
}

// maybeCleanupCache clears the residency cache once per TTL so stale entries do
// not cause reads to touch pages the kernel has since evicted.
func (b *fileByteSlice) maybeCleanupCache() {
	now := time.Now().Unix()
	next := b.mincoreNextCleanup.Load()
	if now <= next {
		return
	}
	if !b.mincoreNextCleanup.CompareAndSwap(next, now+int64(mincoreCacheTTL/time.Second)) {
		return
	}
	for i := range b.mincoreBits {
		b.mincoreBits[i].Store(0)
	}
}

// evict drops the mapping's resident pages and clears the residency cache so
// subsequent reads re-probe with mincore. It is intended for benchmarks that
// want to exercise the cold-page path; it is a no-op when mmap is disabled.
func (b *fileByteSlice) evict() error {
	if len(b.data) == 0 {
		return nil
	}
	for i := range b.mincoreBits {
		b.mincoreBits[i].Store(0)
	}
	b.mincoreNextCleanup.Store(0)
	return evictPages(b.data)
}

// Close releases the mmap region and the underlying file descriptor.
func (b *fileByteSlice) Close() error {
	var err error
	if len(b.data) != 0 {
		err = munmapFile(b.data)
		b.data = nil
	}
	if cerr := b.f.Close(); err == nil {
		err = cerr
	}
	return err
}

// subByteSlice is a view into a parent ByteSlice, mirroring RealByteSlice.Sub.
type subByteSlice struct {
	bs     ByteSlice
	off    int
	length int
}

func (s subByteSlice) Len() int { return s.length }

func (s subByteSlice) Range(start, end int) []byte {
	return s.bs.Range(s.off+start, s.off+end)
}

// Ensure fileByteSlice satisfies the interfaces it is used through.
var (
	_ ByteSlice = (*fileByteSlice)(nil)
	_ io.Closer = (*fileByteSlice)(nil)
)
