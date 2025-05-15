// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// An Object is a representation of a data object.
type Object struct {
	dec encoding.Decoder

	sections []*Section
}

// FromBucket opens an Object from the given storage bucket and path.
// FromBucket returns an error if the metadata of the Object cannot be read or
// if the provided ctx times out.
func FromBucket(ctx context.Context, bucket objstore.Bucket, path string) (*Object, error) {
	obj := &Object{dec: encoding.BucketDecoder(bucket, path)}
	if err := obj.init(ctx); err != nil {
		return nil, err
	}
	return obj, nil
}

// FromReadSeeker opens an Object from the given ReaderAt. The size argument
// specifies the size of the data object in bytes. FromReaderAt returns an
// error if the metadata of the Object cannot be read.
func FromReaderAt(r io.ReaderAt, size int64) (*Object, error) {
	obj := &Object{dec: encoding.ReaderAtDecoder(r, size)}
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
		reader := o.dec.SectionReader(metadata, sec)

		typ, err := reader.Type()
		if err != nil {
			return fmt.Errorf("getting section %d type: %w", i, err)
		}

		readSections = append(readSections, &Section{
			Type:   SectionType(typ),
			Reader: reader,
		})
	}

	o.sections = readSections
	return nil
}

// Sections returns the list of sections available in the Object. The slice of
// returned sections must not be mutated.
func (o *Object) Sections() []*Section { return o.sections }

// Metadata holds high-level metadata about an [Object].
type Metadata struct {
	StreamsSections int // Number of streams sections in the Object.
	LogsSections    int // Number of logs sections in the Object.
}

// Metadata returns the metadata of the Object. Metadata returns an error if
// the object cannot be read.
func (o *Object) Metadata(ctx context.Context) (Metadata, error) {
	var res Metadata
	for _, s := range o.sections {
		switch s.Type {
		case SectionType(encoding.SectionTypeStreams):
			res.StreamsSections++
		case SectionType(encoding.SectionTypeLogs):
			res.LogsSections++
		}
	}
	return res, nil
}

// StreamsDecoder returns a [streams.Decoder] for the given streams section
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

// LogsDecoder returns a [logs.Decoder] for the given logs section index.
// LogsDecoder returns an error if the section is not a logs section.
func (o *Object) LogsDecoder(ctx context.Context, index int) (*logs.Decoder, error) {
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

		if typ == encoding.SectionTypeLogs {
			if found == index {
				reader := o.dec.SectionReader(metadata, s)
				return logs.NewDecoder(reader)
			}
			found++
		}
	}

	return nil, fmt.Errorf("logs section %d not found", index)
}
