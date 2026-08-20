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
	rr  rangeReader
	dec *decoder

	metadata *filemd.Metadata
	sections []*Section
	tenants  []string
}

// MetadataCache caches the metadata prefix of data objects, keyed by an opaque key. Data objects are
// immutable, so a cached entry never goes stale. Implementations must be safe for concurrent use.
type MetadataCache interface {
	// GetMetadata returns the metadata prefix for key. On a miss it calls load, stores the result, and
	// returns it; concurrent calls for the same key should share a single load.
	//
	// The returned slice is read-only and may be shared between concurrent callers (a coalesced miss hands
	// them the same backing array). Callers may retain it but must not mutate it; clone first to modify.
	GetMetadata(ctx context.Context, key string, load func(ctx context.Context) ([]byte, error)) ([]byte, error)
}

// OpenOption customizes how an Object is opened.
type OpenOption func(*openOptions)

type openOptions struct {
	metadataCache MetadataCache
}

// WithMetadataCache serves the object's metadata prefix through cache instead of reading it from object
// storage on every open. A nil cache disables it — pass a nil MetadataCache interface, not a typed-nil
// concrete value (which would satisfy the interface and panic on use).
func WithMetadataCache(cache MetadataCache) OpenOption {
	return func(o *openOptions) { o.metadataCache = cache }
}

// FromBucket opens an Object from the given storage bucket and path.
// FromBucket returns an error if the metadata of the Object cannot be read or
// if the provided ctx times out.
func FromBucket(ctx context.Context, bucket objstore.BucketReader, path string, prefetchBytes int64, opts ...OpenOption) (*Object, error) {
	var o openOptions
	for _, opt := range opts {
		opt(&o)
	}

	rr := &bucketRangeReader{bucket: bucket, path: path}

	dec := &decoder{rr: rr, prefetchBytes: prefetchBytes, metadataCache: o.metadataCache, metadataKey: path}
	obj := &Object{rr: rr, dec: dec}
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
	dec := &decoder{rr: rr, size: size}
	obj := &Object{rr: rr, dec: dec}
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

	sections := make([]*Section, 0, len(metadata.Sections))
	tenants := make(map[string]struct{})

	for i, sec := range metadata.Sections {
		typ, err := getSectionType(metadata, sec)
		if err != nil {
			return fmt.Errorf("getting section %d type: %w", i, err)
		}

		tenant := metadata.Dictionary[sec.TenantRef]
		sections = append(sections, &Section{
			Type:   typ,
			Reader: o.dec.SectionReader(metadata, sec, sec.ExtensionData),
			Tenant: tenant,
		})
		tenants[tenant] = struct{}{}
	}

	o.metadata = metadata
	o.sections = sections
	o.tenants = make([]string, 0, len(tenants))
	for tenant := range tenants {
		o.tenants = append(o.tenants, tenant)
	}

	return nil
}

// Size returns the size of the data object in bytes.
func (o *Object) Size() int64 {
	// By the time Size is called, objectSize would already be using a cached
	// value (by opening the object). The call shouldn't fail, since if
	// we couldn't retrieve the size, the open would've already failed and not
	// returned an object.
	sz, err := o.dec.objectSize(context.Background())
	if err != nil {
		panic(err)
	}
	return sz
}

// Sections returns the list of sections available in the Object. The slice of
// returned sections must not be mutated.
func (o *Object) Sections() Sections { return o.sections }

// Tenant returns the list of tenant that have sections in the Object. The slice of
// returned tenants must not be mutated.
func (o *Object) Tenants() []string { return o.tenants }

// Reader returns a reader for the entire raw data object.
func (o *Object) Reader(ctx context.Context) (io.ReadCloser, error) {
	return o.rr.Read(ctx)
}
