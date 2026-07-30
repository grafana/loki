package index

import (
	stderrors "errors"

	"github.com/prometheus/prometheus/tsdb/fileutil"
)

// This file contains the legacy memory-mapped index reader implementation.
// It is kept behind the "mmap" reader backend (see WithMmap / IndexReaderMmap)
// so it can be selected as an alternative to the default pread-based pool reader
// implemented in file_pool.go.
//
// The mmap reader maps the whole index file into the process address space and
// serves every ByteSlice.Range as a zero-copy sub-slice of the mapping. This is
// very fast when the file stays resident in the page cache, but makes the
// process' memory usage depend on the kernel page cache and can stall goroutines
// on major page faults once the file no longer fits in available memory.

// newMmapFileReader opens the index file at path by memory-mapping it. The
// returned Reader owns the mapping and unmaps it on Close.
func newMmapFileReader(path string) (*Reader, error) {
	f, err := fileutil.OpenMmapFile(path)
	if err != nil {
		return nil, err
	}
	r, err := newReader(RealByteSlice(f.Bytes()), f)
	if err != nil {
		return nil, stderrors.Join(
			err,
			f.Close(),
		)
	}
	return r, nil
}
