//go:build linux

package index

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// hasMincore reports whether page-residency probing is available. On Linux it
// always is.
func hasMincore() bool { return true }

// mincore reports whether the single page starting at addr is resident in the
// page cache. addr must be page-aligned. A non-resident page (or any error)
// yields false, causing the caller to fall back to pread.
func mincore(addr *byte, length int) bool {
	var vec [1]byte
	_, _, errno := unix.Syscall(
		unix.SYS_MINCORE,
		uintptr(unsafe.Pointer(addr)),
		uintptr(length),
		uintptr(unsafe.Pointer(&vec[0])),
	)
	if errno != 0 {
		return false
	}
	// The least significant bit indicates residency.
	return vec[0]&1 == 1
}

// mmapFile maps the file read-only. The mapping length is rounded up to a whole
// number of pages, as recommended by `man 2 mmap`, to reduce the risk of SIGBUS
// when an optimized copy reads slightly past the logical end of the file.
func mmapFile(f *os.File, size int) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	pageSize := os.Getpagesize()
	mapSize := size
	if rem := mapSize % pageSize; rem != 0 {
		mapSize += pageSize - rem
	}
	return unix.Mmap(int(f.Fd()), 0, mapSize, unix.PROT_READ, unix.MAP_SHARED)
}

func munmapFile(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Munmap(data)
}

// evictPages asks the kernel to drop the resident pages of the mapping
// (MADV_DONTNEED). It is used by benchmarks to simulate a cold page cache so
// the mmap read path is forced to take major page faults. It is a no-op when
// there is no mapping.
func evictPages(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return unix.Madvise(data, unix.MADV_DONTNEED)
}
