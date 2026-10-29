//go:build linux

package builder

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// adviseHugePageRange asks the kernel to back b with transparent huge pages
// (MADV_HUGEPAGE) and to collapse what is already resident (MADV_COLLAPSE).
// Best effort: the range is trimmed to whole pages and errors are ignored, so
// kernels without THP or MADV_COLLAPSE keep regular pages.
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
	_ = unix.Madvise(r, unix.MADV_COLLAPSE)
}
