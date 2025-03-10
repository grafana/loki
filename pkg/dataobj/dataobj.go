// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

// An Object is a representation of a data object.
type Object struct {
	dec encoding.Decoder
}

// FromBucket opens an Object from the given storage bucket and path.
func FromBucket(bucket objstore.Bucket, path string) *Object {
	return &Object{dec: encoding.BucketDecoder(bucket, path)}
}

// FromReadSeeker opens an Object from the given ReaderAt. The size argument
// specifies the size of the data object in bytes.
func FromReaderAt(r io.ReaderAt, size int64) *Object {
	return &Object{dec: encoding.ReaderAtDecoder(r, size)}
}

// Metadata holds high-level metadata about an [Object].
type Metadata struct {
	StreamsSections int // Number of streams sections in the Object.
	LogsSections    int // Number of logs sections in the Object.
}

// Metadata returns the metadata of the Object. Metadata returns an error if
// the object cannot be read.
func (o *Object) Metadata(ctx context.Context) (Metadata, error) {
	si, err := o.dec.Sections(ctx)
	if err != nil {
		return Metadata{}, fmt.Errorf("reading sections: %w", err)
	}

	var md Metadata
	for _, s := range si {
		switch s.Type {
		case filemd.SECTION_TYPE_STREAMS:
			md.StreamsSections++
		case filemd.SECTION_TYPE_LOGS:
			md.LogsSections++
		}
	}
	return md, nil
}
