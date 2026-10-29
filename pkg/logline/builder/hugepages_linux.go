//go:build linux

package builder

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const hugePageSize = 2 << 20

// adviseHugePageRange asks the kernel to back b with transparent huge pages.
// Best effort: the range is trimmed to whole pages and errors are ignored, so
// kernels without THP or MADV_COLLAPSE keep regular pages.
//
// MADV_HUGEPAGE alone is not enough for recycled Go heap memory: MADV_DONTNEED
// frees pages but keeps their page tables, and faults under an existing page
// table get 4 KiB pages. MADV_COLLAPSE fixes that, but only collapses regions
// that already hold a writable page, so one page per huge page is faulted in
// first.
func adviseHugePageRange(b []byte) {
	if len(b) == 0 {
		return
	}
	page := uintptr(os.Getpagesize())
	start := uintptr(unsafe.Pointer(&b[0]))
	end := start + uintptr(len(b))
	alignedStart := (start + page - 1) &^ (page - 1)
	alignedEnd := end &^ (page - 1)
	if alignedEnd <= alignedStart {
		return
	}
	r := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&b[0]), alignedStart-start)), alignedEnd-alignedStart)
	_ = unix.Madvise(r, unix.MADV_HUGEPAGE)
	for off := (hugePageSize - alignedStart%hugePageSize) % hugePageSize; off < uintptr(len(r)); off += hugePageSize {
		r[off] = 0
	}
	_ = unix.Madvise(r, unix.MADV_COLLAPSE)
}
