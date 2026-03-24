//go:build !windows && !plan9 && !js

package tsdb

import "golang.org/x/sys/unix"

// madviseWillNeed advises the kernel to asynchronously prefetch the given
// mmap'd byte slice into physical memory, avoiding synchronous major page
// faults on first access.
func madviseWillNeed(b []byte) error {
	return unix.Madvise(b, unix.MADV_WILLNEED)
}
