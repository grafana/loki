// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// This file contains the Loader API for reading YAML documents.
//
// Primary functions:
// - Load: Decode YAML document(s) into a value (use WithAll for multi-doc)
// - NewLoader: Create a streaming loader from io.Reader

package yaml

import (
	"bytes"
	"errors"
	"io"
	"reflect"

	"go.yaml.in/yaml/v4/internal/libyaml"
)

// Load decodes YAML document(s) with the given options.
//
// By default, Load requires exactly one document in the input.
// If zero documents are found, it returns an error.
// If multiple documents are found, it returns an error.
//
// Use WithAllDocuments() to load all documents into a slice:
//
//	var configs []Config
//	yaml.Load(multiDocYAML, &configs, yaml.WithAllDocuments())
//
// When WithAllDocuments is used, out must be a pointer to a slice.
// Each document is decoded into the slice element type.
// Zero documents results in an empty slice (no error).
//
// Maps and pointers (to a struct, string, int, etc) are accepted as out
// values. If an internal pointer within a struct is not initialized,
// the yaml package will initialize it if necessary. The out parameter
// must not be nil.
//
// The type of the decoded values should be compatible with the respective
// values in out. If one or more values cannot be decoded due to type
// mismatches, decoding continues partially until the end of the YAML
// content, and a *yaml.LoadErrors is returned with details for all
// missed values.
//
// Struct fields are only loaded if they are exported (have an upper case
// first letter), and are loaded using the field name lowercased as the
// default key. Custom keys may be defined via the "yaml" name in the field
// tag: the content preceding the first comma is used as the key, and the
// following comma-separated options control the loading and dumping behavior.
//
// For example:
//
//	type T struct {
//	    F int `yaml:"a,omitempty"`
//	    B int
//	}
//	var t T
//	yaml.Load([]byte("a: 1\nb: 2"), &t)
//
// See the documentation of Dump for the format of tags and a list of
// supported tag options.
func Load(in []byte, out any, opts ...Option) error {
	o, err := libyaml.ApplyOptions(opts...)
	if err != nil {
		return err
	}

	if o.AllDocuments {
		// Multi-document mode: out must be pointer to slice
		return loadAll(in, out, o)
	}

	// Single-document mode: exactly one document required
	return loadSingle(in, out, o)
}

// loadAll loads all documents into a slice
func loadAll(in []byte, out any, opts *libyaml.Options) error {
	outVal := reflect.ValueOf(out)
	if outVal.Kind() != reflect.Pointer || outVal.IsNil() {
		return &LoadErrors{Errors: []*libyaml.ConstructError{{
			Err: errors.New("yaml: WithAllDocuments requires a non-nil pointer to a slice"),
		}}}
	}

	sliceVal := outVal.Elem()
	if sliceVal.Kind() != reflect.Slice {
		return &LoadErrors{Errors: []*libyaml.ConstructError{{
			Err: errors.New("yaml: WithAllDocuments requires a pointer to a slice"),
		}}}
	}

	// Create a new slice (clear existing content)
	sliceVal.Set(reflect.MakeSlice(sliceVal.Type(), 0, 0))

	l, err := NewLoader(bytes.NewReader(in), func(o *libyaml.Options) error {
		*o = *opts // Copy options
		return nil
	})
	if err != nil {
		return err
	}

	elemType := sliceVal.Type().Elem()
	for {
		// Create new element of slice's element type
		elemPtr := reflect.New(elemType)
		err := l.Load(elemPtr.Interface())
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Append decoded element to slice
		sliceVal.Set(reflect.Append(sliceVal, elemPtr.Elem()))
	}

	return nil
}

// loadSingle loads exactly one document (strict)
func loadSingle(in []byte, out any, opts *libyaml.Options) error {
	l, err := NewLoader(bytes.NewReader(in), func(o *libyaml.Options) error {
		*o = *opts // Copy options
		return nil
	})
	if err != nil {
		return err
	}

	// Load first document
	err = l.Load(out)
	if err == io.EOF {
		return &LoadErrors{Errors: []*libyaml.ConstructError{{
			Err: errors.New("yaml: no documents in stream"),
		}}}
	}
	if err != nil {
		return err
	}

	// Check for additional documents
	var dummy any
	err = l.Load(&dummy)
	if err != io.EOF {
		if err != nil {
			// Some other error occurred
			return err
		}
		// Successfully loaded a second document - this is an error in strict mode
		return &LoadErrors{Errors: []*libyaml.ConstructError{{
			Err: errors.New("yaml: expected single document, found multiple"),
		}}}
	}

	return nil
}

// A Loader reads and decodes YAML values from an input stream with configurable
// options.
type Loader struct {
	composer *libyaml.Composer
	decoder  *libyaml.Constructor
	opts     *libyaml.Options
	docCount int
}

// NewLoader returns a new Loader that reads from r with the given options.
//
// The Loader introduces its own buffering and may read data from r beyond the
// YAML values requested.
func NewLoader(r io.Reader, opts ...Option) (*Loader, error) {
	o, err := libyaml.ApplyOptions(opts...)
	if err != nil {
		return nil, err
	}
	c := libyaml.NewComposerFromReader(r)
	c.SetStreamNodes(o.StreamNodes)
	return &Loader{
		composer: c,
		decoder:  libyaml.NewConstructor(o),
		opts:     o,
	}, nil
}

// Load reads the next YAML-encoded document from its input and stores it
// in the value pointed to by v.
//
// Returns io.EOF when there are no more documents to read.
// If WithSingleDocument option was set and a document was already read,
// subsequent calls return io.EOF.
//
// Maps and pointers (to a struct, string, int, etc) are accepted as v
// values. If an internal pointer within a struct is not initialized,
// the yaml package will initialize it if necessary. The v parameter
// must not be nil.
//
// Struct fields are only loaded if they are exported (have an upper case
// first letter), and are loaded using the field name lowercased as the
// default key. Custom keys may be defined via the "yaml" name in the field
// tag: the content preceding the first comma is used as the key, and the
// following comma-separated options control the loading and dumping behavior.
//
// See the documentation of the package-level Load function for more details
// about YAML to Go conversion and tag options.
func (l *Loader) Load(v any) (err error) {
	defer handleErr(&err)
	if l.opts.SingleDocument && l.docCount > 0 {
		return io.EOF
	}
	node := l.composer.Parse() // *libyaml.Node
	if node == nil {
		return io.EOF
	}
	l.docCount++

	out := reflect.ValueOf(v)
	if out.Kind() == reflect.Pointer && !out.IsNil() {
		out = out.Elem()
	}
	l.decoder.Construct(node, out) // Pass libyaml.Node directly
	if len(l.decoder.TypeErrors) > 0 {
		typeErrors := l.decoder.TypeErrors
		l.decoder.TypeErrors = nil
		return &LoadErrors{Errors: typeErrors}
	}
	return nil
}
