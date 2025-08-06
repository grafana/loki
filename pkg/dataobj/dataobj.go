// Package dataobj holds utilities for working with data objects.
//
// Data objects are a container format for storing data intended to be
// retrieved from object storage. Each data object is composed of one or more
// "sections," each of which contains a specific type of data, such as logs
// stored in a columnar format.
//
// Sections are further split into two "regions": the section data and the
// section metadata. Section metadata is intended to be a lightweight payload
// per section (usually protobuf) which aids in reading smaller portions of the
// data region.
//
// Each section has a type indicating what kind of data was encoded in that
// section. A data object may have multiple sections of the same type.
//
// The dataobj package provides a low-level [Builder] interface for composing
// sections into a dataobj, and a [SectionReader] interface for reading encoded
// sections.
//
// Section implementations are stored in their own packages and provide
// higher-level utilities for writing and reading those sections. See
// [github.com/grafana/loki/v3/pkg/dataobj/sections/logs] for an example.
//
// # Creating a new section implementation
//
// To create a new section implementation:
//
//  1. Create a new package for your section.
//
//  2. Create a new [SectionType] for your section. Pick a combination of
//     namespace and kind that avoids collisions with other sections that may
//     be written to a file.
//
//  3. Create a "Builder" type which implmeents [SectionBuilder]. Your builder
//     type should have methods for buffering data to be appended. Encode buffered
//     data on a call to Flush.
//
//  4. Create higher-level reading APIs on top of [SectionReader], decoding the
//     data encoded from your builder.
//
// Then, callers can create an instance of your Builder and store it in a data
// object using [Builder.Append], and read it back using your higher-level
// APIs.
//
// While not required, it is typical for section packages to additionally
// implement:
//
//   - A package-level CheckSection function to check if a [Section] was built
//     with your package.
//
//   - A package-specific Section type that wraps [Section] to use in your
//     reading APIs.
//
//   - A function which returns the estimated size of something to be appended
//     to your builder, so callers can flush the section once it gets big enough.
//
//   - A method on your builder to report the estimated size of the section, both
//     before and after compression.
//
// ## Encoding and decoding
//
// There are no requirements on how to encode or decode a section, but there
// are recommendations:
//
//   - Use protobuf for representing the metadata of the section.
//
//   - Write section data so that it can be read in smaller units. For example,
//     columnar data is split into pages, each of which can be read
//     independently.
//
// [SectionReader]s cannot access data outside of their section. Calling
// DataRange with an offset of 0 reads data from the beginning of that
// section's data region, not from the start of the dataobj.
package dataobj

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

// An Object is a representation of a data object.
type Object struct {
	rr   rangeReader
	dec  *decoder
	size int64

	metadata *filemd.Metadata
	sections []*Section
}

// FromBucket opens an Object from the given storage bucket and path.
// FromBucket returns an error if the metadata of the Object cannot be read or
// if the provided ctx times out.
func FromBucket(ctx context.Context, bucket objstore.BucketReader, path string) (*Object, error) {
	rr := &bucketRangeReader{bucket: bucket, path: path}
	size, err := rr.Size(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting size: %w", err)
	}

	dec := &decoder{rr: rr}
	obj := &Object{rr: rr, dec: dec, size: size}
	if err := obj.init(ctx); err != nil {
		return nil, err
	}
	return obj, nil
}

// FromReadSeeker opens an Object from the given ReaderAt. The size argument
// specifies the size of the data object in bytes. FromReaderAt returns an
// error if the metadata of the Object cannot be read.
func FromReaderAt(r io.ReaderAt, size int64) (*Object, error) {
	rr := &readerAtRangeReader{size: size, r: r}
	dec := &decoder{rr: rr}
	obj := &Object{rr: rr, dec: dec, size: size}
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
		typ, err := getSectionType(metadata, sec)
		if err != nil {
			return fmt.Errorf("getting section %d type: %w", i, err)
		}

		readSections = append(readSections, &Section{
			Type:   typ,
			Reader: o.dec.SectionReader(metadata, sec),
		})
	}

	o.metadata = metadata
	o.sections = readSections
	return nil
}

// Size returns the size of the data object in bytes.
func (o *Object) Size() int64 { return o.size }

// Sections returns the list of sections available in the Object. The slice of
// returned sections must not be mutated.
func (o *Object) Sections() Sections { return o.sections }

// Reader returns a reader for the entire raw data object.
func (o *Object) Reader(ctx context.Context) (io.ReadCloser, error) {
	return o.rr.Read(ctx)
}
