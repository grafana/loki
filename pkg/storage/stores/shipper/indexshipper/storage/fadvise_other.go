//go:build !linux

package storage

import "os"

// fadviseDropCache is a no-op on non-Linux platforms where posix_fadvise
// FADV_DONTNEED is not available.
func fadviseDropCache(_ *os.File) error {
	return nil
}
