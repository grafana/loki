// Copyright 2026 The Memory Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory // import "modernc.org/memory"

// canDecommit gates the decommit policy: pooled regions past the hot window
// hand their pages back to the OS.
const canDecommit = true

// decommit returns the physical pages and the commit charge backing the empty
// region at p to the OS without releasing the address reservation - the
// windows analogue of the unix madvise(MADV_DONTNEED) path. Returning the
// commit charge matters more here than the working set: Windows does not
// overcommit, so a drained pool that stayed committed would hold
// pagefile-backed commit against the machine-global limit for as long as it
// is retained.
//
// The first OS page stays committed because it carries the page header.
// Unlike MADV_DONTNEED'ed pages, MEM_DECOMMIT'ed pages fault on touch instead
// of soft-faulting zeroes, so the reuse path recommits them via recommit
// before the region is handed out; the pool's cold watermark is what knows
// when that is needed.
//
// A failed VirtualFree leaves the pages committed, which is safe: the region
// is merely counted as decommitted, and recommit of already-committed pages
// succeeds and preserves them.
func decommit(p uintptr, size int) {
	if size <= osPageSize {
		return
	}

	procVirtualFree.Call(p+uintptr(osPageSize), uintptr(size-osPageSize), _MEM_DECOMMIT)
}

// recommit commits, zero-filled, the pages decommit returned, making a region
// popped from the pool's decommitted prefix usable again. It can fail when
// the machine is out of commit; the caller then leaves the pool untouched and
// reports the failure.
func recommit(p uintptr, size int) error {
	if size <= osPageSize {
		return nil
	}

	addr, _, err := procVirtualAlloc.Call(p+uintptr(osPageSize), uintptr(size-osPageSize), _MEM_COMMIT, _PAGE_READWRITE)
	if addr == 0 {
		return err
	}

	return nil
}
