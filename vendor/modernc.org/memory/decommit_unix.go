// Copyright 2026 The Memory Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package memory // import "modernc.org/memory"

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// canDecommit gates the decommit policy: pooled regions past the hot window
// hand their pages back to the OS.
const canDecommit = true

// madviseAdvice is the advice passed for a region falling out of the hot
// window.
//
// MADV_DONTNEED rather than MADV_FREE: MADV_FREE reclaims lazily, so the
// resident set does not actually drop until the machine is under pressure
// - measured, a 2 GiB pool still read as 2 GiB of RSS. That is the same
// reason the Go runtime defaults to MADV_DONTNEED.
const madviseAdvice = unix.MADV_DONTNEED

// decommit asks the kernel to reclaim the physical pages backing the empty
// region at p without unmapping it. The mapping stays, and with it the address,
// the VMA and the absence of a future mmap - that is the whole point: retention
// keeps its syscall and map-count savings while handing the resident set back.
//
// MADV_DONTNEED does not change vm_flags, so this does not split the VMA.
//
// The first OS page is left alone because it carries the page header, so
// nothing has to be reconstructed when the region comes back out of the pool.
// That holds only while osPageSize < pageSize; on a kernel configured with
// 64 KiB base pages the two are equal and decommit turns itself off rather than
// zeroing a live header. Such hosts would instead need page.size restored from
// the pool's map key in the reuse path.
func decommit(p uintptr, size int) {
	if size <= osPageSize {
		return
	}

	n := size - osPageSize
	unix.Madvise((*rawmem)(unsafe.Pointer(p + uintptr(osPageSize)))[:n:n], madviseAdvice)
}

// recommit is what makes a region from the pool's decommitted prefix usable
// again before it is handed out. On unix there is nothing to do: pages
// discarded by MADV_DONTNEED soft-fault back zero-filled on first touch.
func recommit(uintptr, int) error { return nil }
