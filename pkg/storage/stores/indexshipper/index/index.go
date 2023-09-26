package index

import "io"

type Index interface {
	Name() string
	Path() string
	Close() error
	Reader() (io.ReadSeeker, error)
}

// OpenIndexFileFunc opens an index file stored at the given path.
// There is a possibility of files being corrupted due to abrupt shutdown so
// the implementation should take care of gracefully handling failures in opening corrupted files.
type OpenIndexFileFunc func(string) (Index, error)
type ForEachIndexCallback func(isMultiTenantIndex bool, idx Index) error
