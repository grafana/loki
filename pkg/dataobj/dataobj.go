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
	dec *decoder

	metadata *filemd.Metadata
	sections []*Section
}

// FromBucket opens an Object from the given storage bucket and path.
// FromBucket returns an error if the metadata of the Object cannot be read or
// if the provided ctx times out.
func FromBucket(ctx context.Context, bucket objstore.BucketReader, path string) (*Object, error) {
	dec := &decoder{rr: &bucketRangeReader{bucket: bucket, path: path}}
	obj := &Object{dec: dec}
	if err := obj.init(ctx); err != nil {
		return nil, err
	}
	return obj, nil
}

// FromReadSeeker opens an Object from the given ReaderAt. The size argument
// specifies the size of the data object in bytes. FromReaderAt returns an
// error if the metadata of the Object cannot be read.
func FromReaderAt(r io.ReaderAt, size int64) (*Object, error) {
	dec := &decoder{rr: &readerAtRangeReader{size: size, r: r}}
	obj := &Object{dec: dec}
	if err := obj.init(context.Background()); err != nil {
		return nil, err
	}
	return obj, nil
}

func (o *Object) init(ctx context.Context) error {
	metadata, err := o.dec.Metadata(ctx)
	if err != nil {
		return fmt.Errorf("reading metadata: %w", err)
	}

	readSections := make([]*Section, 0, len(metadata.Sections))
	for i, sec := range metadata.Sections {
		typ, err := encoding.GetSectionType(metadata, sec)
		if err != nil {
			return fmt.Errorf("getting section %d type: %w", i, err)
		}

		readSections = append(readSections, &Section{
			Type:   SectionType(typ),
			Reader: o.dec.SectionReader(metadata, sec),
		})
	}

	o.metadata = metadata
	o.sections = readSections
	return nil
}

// Sections returns the list of sections available in the Object. The slice of
// returned sections must not be mutated.
func (o *Object) Sections() Sections { return o.sections }
