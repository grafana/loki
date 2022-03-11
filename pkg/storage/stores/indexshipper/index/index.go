package index

import "io"

type Index interface {
	Name() string
	Path() string
	Close() error
	Reader() (io.ReadSeeker, error)
}

type OpenIndexFileFunc func(string) (Index, error)
type ForEachIndexCallback func(Index) error
