//go:build linux

package storage

import (
	"os"

	"golang.org/x/sys/unix"
)

// fadviseDropCache advises the kernel that the file's pages are no longer
// needed and can be evicted from the page cache immediately. This prevents
// write-path buffer cache pages from lingering and evicting mmap'd index pages
// under memory pressure.
func fadviseDropCache(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
}
