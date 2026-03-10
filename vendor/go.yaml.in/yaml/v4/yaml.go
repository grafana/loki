// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Package yaml implements YAML support for the Go language.
//
// Source code and other details for the project are available at GitHub:
//
//	https://github.com/yaml/go-yaml
//
// This file contains:
// - Version presets (V2, V3, V4)
// - Options API (WithIndent, WithKnownFields, etc.)
// - Type and constant re-exports from internal/libyaml
// - Helper functions for struct field handling
// - Classic APIs (Decoder, Encoder, Unmarshal, Marshal)
//
// For the main API, see:
// - loader.go: Load, Loader
// - dumper.go: Dump, Dumper

package yaml

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4/internal/libyaml"
)

//-----------------------------------------------------------------------------
// Version presets
//-----------------------------------------------------------------------------

// Usage:
//	yaml.Dump(&data, yaml.V3)
//	yaml.Dump(&data, yaml.V3, yaml.WithIndent(2), yaml.WithCompactSeqIndent())

// V2 defaults:
var V2 = Options(
	WithIndent(2),
	WithCompactSeqIndent(false),
	WithLineWidth(80),
	WithUnicode(true),
	WithUniqueKeys(true),
	WithQuotePreference(QuoteLegacy),
)

// V3 defaults:
var V3 = Options(
	WithIndent(4),
	WithCompactSeqIndent(false),
	WithLineWidth(80),
	WithUnicode(true),
	WithUniqueKeys(true),
	WithQuotePreference(QuoteLegacy),
)

// V4 defaults:
var V4 = Options(
	WithIndent(2),
	WithCompactSeqIndent(true),
	WithLineWidth(80),
	WithUnicode(true),
	WithUniqueKeys(true),
	WithQuotePreference(QuoteSingle),
)

//-----------------------------------------------------------------------------
// Options
//-----------------------------------------------------------------------------

// Option allows configuring YAML loading and dumping operations.
// Re-exported from internal/libyaml.
type Option = libyaml.Option

var (
	// WithIndent sets indentation spaces (2-9).
	// See internal/libyaml.WithIndent.
	WithIndent = libyaml.WithIndent
	// WithCompactSeqIndent configures '- ' as part of indentation.
	// See internal/libyaml.WithCompactSeqIndent.
	WithCompactSeqIndent = libyaml.WithCompactSeqIndent
	// WithKnownFields enables strict field checking during loading.
	// See internal/libyaml.WithKnownFields.
	WithKnownFields = libyaml.WithKnownFields
	// WithSingleDocument only processes first document in stream.
	// See internal/libyaml.WithSingleDocument.
	WithSingleDocument = libyaml.WithSingleDocument
	// WithStreamNodes enables stream boundary nodes when loading.
	// See internal/libyaml.WithStreamNodes.
	WithStreamNodes = libyaml.WithStreamNodes
	// WithAllDocuments enables multi-document mode for Load and Dump.
	// See internal/libyaml.WithAllDocuments.
	WithAllDocuments = libyaml.WithAllDocuments
	// WithLineWidth sets preferred line width for output.
	// See internal/libyaml.WithLineWidth.
	WithLineWidth = libyaml.WithLineWidth
	// WithUnicode controls non-ASCII characters in output.
	// See internal/libyaml.WithUnicode.
	WithUnicode = libyaml.WithUnicode
	// WithUniqueKeys enables duplicate key detection.
	// See internal/libyaml.WithUniqueKeys.
	WithUniqueKeys = libyaml.WithUniqueKeys
	// WithCanonical forces canonical YAML output format.
	// See internal/libyaml.WithCanonical.
	WithCanonical = libyaml.WithCanonical
	// WithLineBreak sets line ending style for output.
	// See internal/libyaml.WithLineBreak.
	WithLineBreak = libyaml.WithLineBreak
	// WithExplicitStart controls document start markers (---).
	// See internal/libyaml.WithExplicitStart.
	WithExplicitStart = libyaml.WithExplicitStart
	// WithExplicitEnd controls document end markers (...).
	// See internal/libyaml.WithExplicitEnd.
	WithExplicitEnd = libyaml.WithExplicitEnd
	// WithFlowSimpleCollections controls flow style for simple collections.
	// See internal/libyaml.WithFlowSimpleCollections.
	WithFlowSimpleCollections = libyaml.WithFlowSimpleCollections
	// WithQuotePreference sets preferred quote style when quoting is required.
	// See internal/libyaml.WithQuotePreference.
	WithQuotePreference = libyaml.WithQuotePreference
)

// Options combines multiple options into a single Option.
// This is useful for creating option presets or combining version defaults
// with custom options.
//
// Example:
//
//	opts := yaml.Options(yaml.V4, yaml.WithIndent(3))
//	yaml.Dump(&data, opts)
func Options(opts ...Option) Option {
	return libyaml.CombineOptions(opts...)
}

// OptsYAML parses a YAML string containing option settings and returns
// an Option that can be combined with other options using Options().
//
// The YAML string can specify any of these fields:
// - indent (int)
// - compact-seq-indent (bool)
// - line-width (int)
// - unicode (bool)
// - canonical (bool)
// - line-break (string: ln, cr, crln)
// - explicit-start (bool)
// - explicit-end (bool)
// - flow-simple-coll (bool)
// - known-fields (bool)
// - single-document (bool)
// - unique-keys (bool)
//
// Only fields specified in the YAML will override other options when
// combined. Unspecified fields won't affect other options.
//
// Example:
//
//	opts, err := yaml.OptsYAML(`
//	  indent: 3
//	  known-fields: true
//	`)
//	yaml.Dump(&data, yaml.Options(V4, opts))
func OptsYAML(yamlStr string) (Option, error) {
	var cfg struct {
		Indent                *int    `yaml:"indent"`
		CompactSeqIndent      *bool   `yaml:"compact-seq-indent"`
		LineWidth             *int    `yaml:"line-width"`
		Unicode               *bool   `yaml:"unicode"`
		Canonical             *bool   `yaml:"canonical"`
		LineBreak             *string `yaml:"line-break"`
		ExplicitStart         *bool   `yaml:"explicit-start"`
		ExplicitEnd           *bool   `yaml:"explicit-end"`
		FlowSimpleCollections *bool   `yaml:"flow-simple-coll"`
		KnownFields           *bool   `yaml:"known-fields"`
		SingleDocument        *bool   `yaml:"single-document"`
		UniqueKeys            *bool   `yaml:"unique-keys"`
	}
	if err := Load([]byte(yamlStr), &cfg, WithKnownFields()); err != nil {
		return nil, err
	}

	// Build options only for fields that were set
	var optList []Option
	if cfg.Indent != nil {
		optList = append(optList, WithIndent(*cfg.Indent))
	}
	if cfg.CompactSeqIndent != nil {
		optList = append(optList, WithCompactSeqIndent(*cfg.CompactSeqIndent))
	}
	if cfg.LineWidth != nil {
		optList = append(optList, WithLineWidth(*cfg.LineWidth))
	}
	if cfg.Unicode != nil {
		optList = append(optList, WithUnicode(*cfg.Unicode))
	}
	if cfg.ExplicitStart != nil {
		optList = append(optList, WithExplicitStart(*cfg.ExplicitStart))
	}
	if cfg.ExplicitEnd != nil {
		optList = append(optList, WithExplicitEnd(*cfg.ExplicitEnd))
	}
	if cfg.FlowSimpleCollections != nil {
		optList = append(optList, WithFlowSimpleCollections(*cfg.FlowSimpleCollections))
	}
	if cfg.KnownFields != nil {
		optList = append(optList, WithKnownFields(*cfg.KnownFields))
	}
	if cfg.SingleDocument != nil && *cfg.SingleDocument {
		optList = append(optList, WithSingleDocument())
	}
	if cfg.UniqueKeys != nil {
		optList = append(optList, WithUniqueKeys(*cfg.UniqueKeys))
	}
	if cfg.Canonical != nil {
		optList = append(optList, WithCanonical(*cfg.Canonical))
	}
	if cfg.LineBreak != nil {
		switch *cfg.LineBreak {
		case "ln":
			optList = append(optList, WithLineBreak(LineBreakLN))
		case "cr":
			optList = append(optList, WithLineBreak(LineBreakCR))
		case "crln":
			optList = append(optList, WithLineBreak(LineBreakCRLN))
		default:
			return nil, errors.New("yaml: invalid line-break value (use ln, cr, or crln)")
		}
	}

	return Options(optList...), nil
}

//-----------------------------------------------------------------------------
// Type and constant re-exports
//-----------------------------------------------------------------------------

type (
	// Node represents a YAML node in the document tree.
	// See internal/libyaml.Node.
	Node = libyaml.Node
	// Kind identifies the type of a YAML node.
	// See internal/libyaml.Kind.
	Kind = libyaml.Kind
	// Style controls the presentation of a YAML node.
	// See internal/libyaml.Style.
	Style = libyaml.Style
	// Marshaler is implemented by types with custom YAML marshaling.
	// See internal/libyaml.Marshaler.
	Marshaler = libyaml.Marshaler
	// IsZeroer is implemented by types that can report if they're zero.
	// See internal/libyaml.IsZeroer.
	IsZeroer = libyaml.IsZeroer
)

// Unmarshaler is the interface implemented by types
// that can unmarshal a YAML description of themselves.
type Unmarshaler interface {
	UnmarshalYAML(node *Node) error
}

// Re-export stream-related types
type (
	VersionDirective = libyaml.StreamVersionDirective
	TagDirective     = libyaml.StreamTagDirective
	Encoding         = libyaml.Encoding
)

// Re-export encoding constants
const (
	EncodingAny     = libyaml.ANY_ENCODING
	EncodingUTF8    = libyaml.UTF8_ENCODING
	EncodingUTF16LE = libyaml.UTF16LE_ENCODING
	EncodingUTF16BE = libyaml.UTF16BE_ENCODING
)

// Re-export error types
type (

	// LoadError represents an error encountered while decoding a YAML document.
	//
	// It contains details about the location in the document where the error
	// occurred, as well as a descriptive message.
	LoadError = libyaml.ConstructError

	// LoadErrors is returned when one or more fields cannot be properly decoded.
	//
	// It contains multiple *[LoadError] instances with details about each error.
	LoadErrors = libyaml.LoadErrors

	// TypeError is an obsolete error type retained for compatibility.
	//
	// Deprecated: Use [LoadErrors] instead.
	//
	//nolint:staticcheck // we are using deprecated TypeError for compatibility
	TypeError = libyaml.TypeError
)

// Re-export Kind constants
const (
	DocumentNode = libyaml.DocumentNode
	SequenceNode = libyaml.SequenceNode
	MappingNode  = libyaml.MappingNode
	ScalarNode   = libyaml.ScalarNode
	AliasNode    = libyaml.AliasNode
	StreamNode   = libyaml.StreamNode
)

// Re-export Style constants
const (
	TaggedStyle       = libyaml.TaggedStyle
	DoubleQuotedStyle = libyaml.DoubleQuotedStyle
	SingleQuotedStyle = libyaml.SingleQuotedStyle
	LiteralStyle      = libyaml.LiteralStyle
	FoldedStyle       = libyaml.FoldedStyle
	FlowStyle         = libyaml.FlowStyle
)

// LineBreak represents the line ending style for YAML output.
type LineBreak = libyaml.LineBreak

// Line break constants for different platforms.
const (
	LineBreakLN   = libyaml.LN_BREAK   // Unix-style \n (default)
	LineBreakCR   = libyaml.CR_BREAK   // Old Mac-style \r
	LineBreakCRLN = libyaml.CRLN_BREAK // Windows-style \r\n
)

// QuoteStyle represents the quote style to use when quoting is required.
type QuoteStyle = libyaml.QuoteStyle

// Quote style constants for required quoting.
const (
	QuoteSingle = libyaml.QuoteSingle // Prefer single quotes (v4 default)
	QuoteDouble = libyaml.QuoteDouble // Prefer double quotes
	QuoteLegacy = libyaml.QuoteLegacy // Legacy v2/v3 behavior
)

//-----------------------------------------------------------------------------
// Helper functions
//-----------------------------------------------------------------------------

// The code in this section was copied from mgo/bson.

var (
	structMap       = make(map[reflect.Type]*structInfo)
	fieldMapMutex   sync.RWMutex
	unmarshalerType reflect.Type
)

// structInfo holds details for the serialization of fields of
// a given struct.
type structInfo struct {
	FieldsMap  map[string]fieldInfo
	FieldsList []fieldInfo

	// InlineMap is the number of the field in the struct that
	// contains an ,inline map, or -1 if there's none.
	InlineMap int

	// InlineUnmarshalers holds indexes to inlined fields that
	// contain unmarshaler values.
	InlineUnmarshalers [][]int
}

type fieldInfo struct {
	Key       string
	Num       int
	OmitEmpty bool
	Flow      bool
	// Id holds the unique field identifier, so we can cheaply
	// check for field duplicates without maintaining an extra map.
	Id int

	// Inline holds the field index if the field is part of an inlined struct.
	Inline []int
}

func getStructInfo(st reflect.Type) (*structInfo, error) {
	fieldMapMutex.RLock()
	sinfo, found := structMap[st]
	fieldMapMutex.RUnlock()
	if found {
		return sinfo, nil
	}

	n := st.NumField()
	fieldsMap := make(map[string]fieldInfo)
	fieldsList := make([]fieldInfo, 0, n)
	inlineMap := -1
	inlineUnmarshalers := [][]int(nil)
	for i := 0; i != n; i++ {
		field := st.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue // Private field
		}

		info := fieldInfo{Num: i}

		tag := field.Tag.Get("yaml")
		if tag == "" && !strings.Contains(string(field.Tag), ":") {
			tag = string(field.Tag)
		}
		if tag == "-" {
			continue
		}

		inline := false
		fields := strings.Split(tag, ",")
		if len(fields) > 1 {
			for _, flag := range fields[1:] {
				switch flag {
				case "omitempty":
					info.OmitEmpty = true
				case "flow":
					info.Flow = true
				case "inline":
					inline = true
				default:
					return nil, fmt.Errorf("unsupported flag %q in tag %q of type %s", flag, tag, st)
				}
			}
			tag = fields[0]
		}

		if inline {
			switch field.Type.Kind() {
			case reflect.Map:
				if inlineMap >= 0 {
					return nil, errors.New("multiple ,inline maps in struct " + st.String())
				}
				if field.Type.Key() != reflect.TypeOf("") {
					return nil, errors.New("option ,inline needs a map with string keys in struct " + st.String())
				}
				inlineMap = info.Num
			case reflect.Struct, reflect.Pointer:
				ftype := field.Type
				for ftype.Kind() == reflect.Pointer {
					ftype = ftype.Elem()
				}
				if ftype.Kind() != reflect.Struct {
					return nil, errors.New("option ,inline may only be used on a struct or map field")
				}
				if reflect.PointerTo(ftype).Implements(unmarshalerType) {
					inlineUnmarshalers = append(inlineUnmarshalers, []int{i})
				} else {
					sinfo, err := getStructInfo(ftype)
					if err != nil {
						return nil, err
					}
					for _, index := range sinfo.InlineUnmarshalers {
						inlineUnmarshalers = append(inlineUnmarshalers, append([]int{i}, index...))
					}
					for _, finfo := range sinfo.FieldsList {
						if _, found := fieldsMap[finfo.Key]; found {
							msg := "duplicated key '" + finfo.Key + "' in struct " + st.String()
							return nil, errors.New(msg)
						}
						if finfo.Inline == nil {
							finfo.Inline = []int{i, finfo.Num}
						} else {
							finfo.Inline = append([]int{i}, finfo.Inline...)
						}
						finfo.Id = len(fieldsList)
						fieldsMap[finfo.Key] = finfo
						fieldsList = append(fieldsList, finfo)
					}
				}
			default:
				return nil, errors.New("option ,inline may only be used on a struct or map field")
			}
			continue
		}

		if tag != "" {
			info.Key = tag
		} else {
			info.Key = strings.ToLower(field.Name)
		}

		if _, found = fieldsMap[info.Key]; found {
			msg := "duplicated key '" + info.Key + "' in struct " + st.String()
			return nil, errors.New(msg)
		}

		info.Id = len(fieldsList)
		fieldsList = append(fieldsList, info)
		fieldsMap[info.Key] = info
	}

	sinfo = &structInfo{
		FieldsMap:          fieldsMap,
		FieldsList:         fieldsList,
		InlineMap:          inlineMap,
		InlineUnmarshalers: inlineUnmarshalers,
	}

	fieldMapMutex.Lock()
	structMap[st] = sinfo
	fieldMapMutex.Unlock()
	return sinfo, nil
}

var noWriter io.Writer

func handleErr(err *error) {
	if v := recover(); v != nil {
		if e, ok := v.(*libyaml.YAMLError); ok {
			*err = e.Err
		} else {
			panic(v)
		}
	}
}

//-----------------------------------------------------------------------------
// Classic APIs
//-----------------------------------------------------------------------------

// A Decoder reads and decodes YAML values from an input stream.
type Decoder struct {
	composer    *libyaml.Composer
	knownFields bool
}

// NewDecoder returns a new decoder that reads from r.
//
// The decoder introduces its own buffering and may read
// data from r beyond the YAML values requested.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		composer: libyaml.NewComposerFromReader(r),
	}
}

// KnownFields ensures that the keys in decoded mappings to
// exist as fields in the struct being decoded into.
func (dec *Decoder) KnownFields(enable bool) {
	dec.knownFields = enable
}

// Decode reads the next YAML-encoded value from its input
// and stores it in the value pointed to by v.
//
// See the documentation for Unmarshal for details about the
// conversion of YAML into a Go value.
func (dec *Decoder) Decode(v any) (err error) {
	d := libyaml.NewConstructor(libyaml.DefaultOptions)
	d.KnownFields = dec.knownFields
	defer handleErr(&err)
	node := dec.composer.Parse()
	if node == nil {
		return io.EOF
	}
	out := reflect.ValueOf(v)
	if out.Kind() == reflect.Pointer && !out.IsNil() {
		out = out.Elem()
	}
	d.Construct(node, out)
	if len(d.TypeErrors) > 0 {
		return &LoadErrors{Errors: d.TypeErrors}
	}
	return nil
}

// An Encoder writes YAML values to an output stream.
type Encoder struct {
	encoder *libyaml.Representer
}

// NewEncoder returns a new encoder that writes to w.
// The Encoder should be closed after use to flush all data
// to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		encoder: libyaml.NewRepresenter(w, libyaml.DefaultOptions),
	}
}

// Encode writes the YAML encoding of v to the stream.
// If multiple items are encoded to the stream, the
// second and subsequent document will be preceded
// with a "---" document separator, but the first will not.
//
// See the documentation for Marshal for details about the conversion of Go
// values to YAML.
func (e *Encoder) Encode(v any) (err error) {
	defer handleErr(&err)
	e.encoder.MarshalDoc("", reflect.ValueOf(v))
	return nil
}

// SetIndent changes the used indentation used when encoding.
func (e *Encoder) SetIndent(spaces int) {
	if spaces < 0 {
		panic("yaml: cannot indent to a negative number of spaces")
	}
	e.encoder.Indent = spaces
}

// CompactSeqIndent makes it so that '- ' is considered part of the indentation.
func (e *Encoder) CompactSeqIndent() {
	e.encoder.Emitter.CompactSequenceIndent = true
}

// DefaultSeqIndent makes it so that '- ' is not considered part of the indentation.
func (e *Encoder) DefaultSeqIndent() {
	e.encoder.Emitter.CompactSequenceIndent = false
}

// Close closes the encoder by writing any remaining data.
// It does not write a stream terminating string "...".
func (e *Encoder) Close() (err error) {
	defer handleErr(&err)
	e.encoder.Finish()
	return nil
}

// Unmarshal decodes the first document found within the in byte slice
// and assigns decoded values into the out value.
//
// Maps and pointers (to a struct, string, int, etc) are accepted as out
// values. If an internal pointer within a struct is not initialized,
// the yaml package will initialize it if necessary for unmarshalling
// the provided data. The out parameter must not be nil.
//
// The type of the decoded values should be compatible with the respective
// values in out. If one or more values cannot be decoded due to a type
// mismatches, decoding continues partially until the end of the YAML
// content, and a *yaml.LoadErrors is returned with details for all
// missed values.
//
// Struct fields are only unmarshalled if they are exported (have an
// upper case first letter), and are unmarshalled using the field name
// lowercased as the default key. Custom keys may be defined via the
// "yaml" name in the field tag: the content preceding the first comma
// is used as the key, and the following comma-separated options are
// used to tweak the marshaling process (see Marshal).
// Conflicting names result in a runtime error.
//
// For example:
//
//	type T struct {
//	    F int `yaml:"a,omitempty"`
//	    B int
//	}
//	var t T
//	yaml.Construct([]byte("a: 1\nb: 2"), &t)
//
// See the documentation of Marshal for the format of tags and a list of
// supported tag options.
func Unmarshal(in []byte, out any) (err error) {
	return unmarshal(in, out, V3)
}

func unmarshal(in []byte, out any, opts ...Option) (err error) {
	defer handleErr(&err)
	o, err := libyaml.ApplyOptions(opts...)
	if err != nil {
		return err
	}

	// Check if out implements yaml.Unmarshaler
	if u, ok := out.(Unmarshaler); ok {
		p := libyaml.NewComposer(in)
		defer p.Destroy()
		node := p.Parse()
		if node != nil {
			return u.UnmarshalYAML(node)
		}
		return nil
	}

	return libyaml.Construct(in, out, o)
}

// Marshal serializes the value provided into a YAML document. The structure
// of the generated document will reflect the structure of the value itself.
// Maps and pointers (to struct, string, int, etc) are accepted as the in value.
//
// Struct fields are only marshaled if they are exported (have an upper case
// first letter), and are marshaled using the field name lowercased as the
// default key. Custom keys may be defined via the "yaml" name in the field
// tag: the content preceding the first comma is used as the key, and the
// following comma-separated options are used to tweak the marshaling process.
// Conflicting names result in a runtime error.
//
// The field tag format accepted is:
//
//	`(...) yaml:"[<key>][,<flag1>[,<flag2>]]" (...)`
//
// The following flags are currently supported:
//
//	omitempty    Only include the field if it's not set to the zero
//	             value for the type or to empty slices or maps.
//	             Zero valued structs will be omitted if all their public
//	             fields are zero, unless they implement an IsZero
//	             method (see the IsZeroer interface type), in which
//	             case the field will be excluded if IsZero returns true.
//
//	flow         Marshal using a flow style (useful for structs,
//	             sequences and maps).
//
//	inline       Inline the field, which must be a struct or a map,
//	             causing all of its fields or keys to be processed as if
//	             they were part of the outer struct. For maps, keys must
//	             not conflict with the yaml keys of other struct fields.
//	             See doc/inline-tags.md for detailed examples and use cases.
//
// In addition, if the key is "-", the field is ignored.
//
// For example:
//
//	type T struct {
//	    F int `yaml:"a,omitempty"`
//	    B int
//	}
//	yaml.Marshal(&T{B: 2}) // Returns "b: 2\n"
//	yaml.Marshal(&T{F: 1}} // Returns "a: 1\nb: 0\n"
func Marshal(in any) (out []byte, err error) {
	defer handleErr(&err)
	e := libyaml.NewRepresenter(noWriter, libyaml.DefaultOptions)
	defer e.Destroy()
	e.MarshalDoc("", reflect.ValueOf(in))
	e.Finish()
	out = e.Out
	return out, err
}
