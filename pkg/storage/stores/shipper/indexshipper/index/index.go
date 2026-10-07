package index

import "io"

type Index interface {
	Name() string
	Path() string
	Close() error
	Reader() (io.ReadSeekCloser, error)
}

// OpenOptions describes why an index file is being opened.
type OpenOptions struct {
	// QueryReady is set when the file belongs to an index set loaded for query
	// readiness (preloaded or kept ready by the periodic loop), rather than one
	// downloaded on demand by a query or found on local disk at startup.
	QueryReady bool
}

// OpenIndexFileFunc opens an index file stored at the given path.
// There is a possibility of files being corrupted due to abrupt shutdown so
// the implementation should take care of gracefully handling failures in opening corrupted files.
type OpenIndexFileFunc func(path string, opts OpenOptions) (Index, error)
type ForEachIndexCallback func(isMultiTenantIndex bool, idx Index) error
