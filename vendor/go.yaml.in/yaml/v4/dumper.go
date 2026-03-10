// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// This file contains the Dumper API for writing YAML documents.
//
// Primary functions:
// - Dump: Encode value(s) to YAML (use WithAll for multi-doc)
// - NewDumper: Create a streaming dumper to io.Writer

package yaml

import (
	"bytes"
	"errors"
	"io"
	"reflect"

	"go.yaml.in/yaml/v4/internal/libyaml"
)

// Dump encodes a value to YAML with the given options.
//
// By default, Dump encodes a single value as a single YAML document.
//
// Use WithAllDocuments() to encode multiple values as a multi-document stream:
//
//	docs := []Config{config1, config2, config3}
//	yaml.Dump(docs, yaml.WithAllDocuments())
//
// When WithAllDocuments is used, in must be a slice.
// Each element is encoded as a separate YAML document with "---" separators.
//
// See [Marshal] for details about the conversion of Go values to YAML.
func Dump(in any, opts ...Option) (out []byte, err error) {
	defer handleErr(&err)

	o, err := libyaml.ApplyOptions(opts...)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	d, err := NewDumper(&buf, func(opts *libyaml.Options) error {
		*opts = *o // Copy options
		return nil
	})
	if err != nil {
		return nil, err
	}

	if o.AllDocuments {
		// Multi-document mode: in must be a slice
		inVal := reflect.ValueOf(in)
		if inVal.Kind() != reflect.Slice {
			return nil, &LoadErrors{Errors: []*libyaml.ConstructError{{
				Err: errors.New("yaml: WithAllDocuments requires a slice input"),
			}}}
		}

		// Dump each element as a separate document
		for i := 0; i < inVal.Len(); i++ {
			if err := d.Dump(inVal.Index(i).Interface()); err != nil {
				return nil, err
			}
		}
	} else {
		// Single-document mode
		if err := d.Dump(in); err != nil {
			return nil, err
		}
	}

	if err := d.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// A Dumper writes YAML values to an output stream with configurable options.
type Dumper struct {
	encoder *libyaml.Representer
	opts    *libyaml.Options
}

// NewDumper returns a new Dumper that writes to w with the given options.
//
// The Dumper should be closed after use to flush all data to w.
func NewDumper(w io.Writer, opts ...Option) (*Dumper, error) {
	o, err := libyaml.ApplyOptions(opts...)
	if err != nil {
		return nil, err
	}
	return &Dumper{
		encoder: libyaml.NewRepresenter(w, o),
		opts:    o,
	}, nil
}

// Dump writes the YAML encoding of v to the stream.
//
// If multiple values are dumped to the stream, the second and subsequent
// documents will be preceded with a "---" document separator.
//
// See the documentation for [Marshal] for details about the conversion of Go
// values to YAML.
func (d *Dumper) Dump(v any) (err error) {
	defer handleErr(&err)
	d.encoder.MarshalDoc("", reflect.ValueOf(v))
	return nil
}

// Close closes the Dumper by writing any remaining data.
// It does not write a stream terminating string "...".
func (d *Dumper) Close() (err error) {
	defer handleErr(&err)
	d.encoder.Finish()
	return nil
}
