package metadata

import (
	"context"
	"errors"
	"io"
)

// ErrNotPrefetchable means the caller must read the metadata of an object directly. The layout is
// invalid, the metadata region is too large, or the cache failed and cannot answer.
var ErrNotPrefetchable = errors.New("data object metadata cannot be prefetched")

// DataobjSourceReader reads byte ranges of one object from object storage.
type DataobjSourceReader interface {
	// ReadRange returns up to length bytes starting at offset. It returns fewer bytes when the
	// object ends first. The caller closes the reader.
	ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)
}

// PrefetchSet says which metadata a query reads. The zero value reads the stream ID and the timestamp.
type PrefetchSet struct {
	// Message is true when the query reads the message column. It is ignored when AllMetadata is true.
	Message bool

	// AllMetadata is true when the query reads every structured metadata column. The prefetcher then
	// serves the whole region, so Message and MetadataNames have no effect.
	AllMetadata bool

	// MetadataNames lists the structured metadata columns the query reads by name. It is ignored when
	// AllMetadata is true.
	MetadataNames []string
}

// PrefetchRequest asks a Prefetcher for the metadata of one object.
type PrefetchRequest struct {
	// Path is the object key, unique within the bucket.
	Path        string
	PrefetchSet PrefetchSet
	Source      DataobjSourceReader
}
