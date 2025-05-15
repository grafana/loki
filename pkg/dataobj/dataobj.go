// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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
	metadata, err := o.dec.Metadata(ctx)
	if err != nil {
		return Metadata{}, fmt.Errorf("reading sections: %w", err)
	}

	var res Metadata
	for _, s := range metadata.Sections {
		typ, err := encoding.GetSectionType(metadata, s)
		if err != nil {
			return Metadata{}, fmt.Errorf("getting section type: %w", err)
		}

		switch typ {
		case encoding.SectionTypeStreams:
			res.StreamsSections++
		case encoding.SectionTypeLogs:
			res.LogsSections++
		}
	}
	return res, nil
}

// StreamsDecoder returns a [Streams.Decoder] for the given streams section
// index. StreamsDecoder returns an error if the section is not a streams
// section.
func (o *Object) StreamsDecoder(ctx context.Context, index int) (*streams.Decoder, error) {
	metadata, err := o.dec.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading sections: %w", err)
	}

	var found int

	for _, s := range metadata.Sections {
		typ, err := encoding.GetSectionType(metadata, s)
		if err != nil {
			return nil, fmt.Errorf("getting section type: %w", err)
		}

		if typ == encoding.SectionTypeStreams {
			if found == index {
				reader := o.dec.SectionReader(metadata, s)
				return streams.NewDecoder(reader)
			}
			found++
		}
	}

	return nil, fmt.Errorf("streams section %d not found", index)
}
