//go:build !linux

package index

import "os"

// hasMincore reports whether page-residency probing is available. Without
// mincore support we cannot safely tell whether an mmap read would fault, so we
// disable mmap and always read via pread.
func hasMincore() bool { return false }

// mincore is never called when hasMincore returns false; it exists only to
// satisfy the shared code path.
func mincore(_ *byte, _ int) bool { return false }

// mmapFile is a no-op on platforms without residency probing. Returning a nil
// mapping forces the reader onto the pread path.
func mmapFile(_ *os.File, _ int) ([]byte, error) { return nil, nil }

func munmapFile(_ []byte) error { return nil }

// evictPages is a no-op on platforms without mmap support.
func evictPages(_ []byte) error { return nil }
