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
// - Version preset functions (WithV2Defaults, WithV3Defaults, WithV4Defaults)
// - Options API (WithIndent, WithKnownFields, etc.)
// - Type and constant re-exports from internal/libyaml
// - Helper functions for struct field handling
// - Load/Dump API (Load, Dump, Loader, Dumper)
// - Classic APIs (Decoder, Encoder, Unmarshal, Marshal)

package yaml

import (
	"errors"
	"fmt"
	"io"

	"github.com/pb33f/go-yaml/internal/libyaml"
	pluginreg "github.com/pb33f/go-yaml/internal/plugin"
	"github.com/pb33f/go-yaml/plugin/limit"
)

//-----------------------------------------------------------------------------
// Version presets
//-----------------------------------------------------------------------------

// Usage:
//	yaml.Dump(&data, yaml.WithV3Defaults())
//	yaml.Dump(&data, yaml.WithV3Defaults(), yaml.WithIndent(2), yaml.WithCompactSeqIndent())

// WithV2Defaults returns V2-compatible default options.
func WithV2Defaults() Option {
	return Options(
		WithIndent(2),
		WithCompactSeqIndent(false),
		WithLineWidth(80),
		WithUnicode(true),
		WithUniqueKeys(true),
		WithQuotePreference(QuoteLegacy),
		WithPlugin(limit.New()),
	)
}

// WithV3Defaults returns V3-compatible default options.
func WithV3Defaults() Option {
	return Options(
		WithIndent(4),
		WithCompactSeqIndent(false),
		WithLineWidth(80),
		WithUnicode(true),
		WithUniqueKeys(true),
		WithQuotePreference(QuoteLegacy),
		WithPlugin(limit.New()),
	)
}

// WithV4Defaults returns the current V4 default options.
func WithV4Defaults() Option {
	return Options(
		WithIndent(2),
		WithCompactSeqIndent(true),
		WithLineWidth(80),
		WithUnicode(true),
		WithUniqueKeys(true),
		WithQuotePreference(QuoteSingle),
		WithPlugin(limit.New()),
	)
}

//-----------------------------------------------------------------------------
// Options
//-----------------------------------------------------------------------------

// Option allows configuring YAML loading and dumping operations.
type Option = libyaml.Option

// Option configuration functions
var (
	// WithIndent sets the number of spaces to use for indentation when
	// dumping YAML content.
	//
	// Valid values are 2-9. Common choices: 2 (compact), 4 (readable).
	WithIndent = libyaml.WithIndent

	// WithCompactSeqIndent configures whether the sequence indicator '- ' is
	// considered part of the indentation when dumping YAML content.
	//
	// If compact is true, '- ' is treated as part of the indentation.
	// If compact is false, '- ' is not treated as part of the indentation.
	// When called without arguments, defaults to true.
	WithCompactSeqIndent = libyaml.WithCompactSeqIndent

	// WithKnownFields enables or disables strict field checking during YAML
	// loading.
	//
	// When enabled, loading will return an error if the YAML input contains
	// fields that do not correspond to any fields in the target struct.
	// When called without arguments, defaults to true.
	WithKnownFields = libyaml.WithKnownFields

	// WithSingleDocument configures the Loader to only process the first
	// document in a YAML stream. After the first document is loaded,
	// subsequent calls to Load will return [io.EOF].
	//
	// When called without arguments, defaults to true.
	//
	// This is useful when you expect exactly one document and want behavior
	// similar to Unmarshal.
	WithSingleDocument = libyaml.WithSingleDocument

	// WithStreamNodes enables returning stream boundary nodes when loading
	// YAML.
	//
	// When enabled, Loader.Load returns an interleaved sequence of
	// StreamNode and DocumentNode values:
	//
	//	[StreamNode, DocNode, StreamNode, DocNode, ..., StreamNode]
	//
	// StreamNodes contain metadata about the stream including:
	//   - Encoding (UTF-8, UTF-16LE, UTF-16BE)
	//   - YAML version directive (%YAML)
	//   - Tag directives (%TAG)
	//   - Position information (Line, Column)
	//
	// An empty YAML stream returns a single StreamNode.
	// When called without arguments, defaults to true.
	//
	// The default is false.
	WithStreamNodes = libyaml.WithStreamNodes

	// WithAllDocuments enables multi-document mode for Load and Dump
	// operations.
	//
	// When used with Load, the target must be a pointer to a slice.
	// All documents in the YAML stream will be decoded into the slice.
	// Zero documents results in an empty slice (no error).
	//
	// When used with Dump, the input must be a slice.
	// Each element will be encoded as a separate YAML document
	// with "---" separators.
	//
	// When called without arguments, defaults to true.
	//
	// The default is false (single-document mode).
	WithAllDocuments = libyaml.WithAllDocuments

	// WithLineWidth sets the preferred line width for YAML output.
	//
	// When encoding long strings, the encoder will attempt to wrap them at
	// this width using literal block style (|). Set to -1 or 0 for unlimited
	// width.
	//
	// The default is 80 characters.
	WithLineWidth = libyaml.WithLineWidth

	// WithUnicode controls whether non-ASCII characters are allowed in YAML
	// output.
	//
	// When true, non-ASCII characters appear as-is (e.g., "café").
	// When false, non-ASCII characters are escaped (e.g., "caf\u00e9").
	// When called without arguments, defaults to true.
	//
	// The default is true.
	WithUnicode = libyaml.WithUnicode

	// WithUniqueKeys enables or disables duplicate key detection during YAML
	// loading.
	//
	// When enabled, loading will return an error if the YAML input contains
	// duplicate keys in any mapping. This is a security feature that prevents
	// key override attacks.
	// When called without arguments, defaults to true.
	//
	// The default is true.
	WithUniqueKeys = libyaml.WithUniqueKeys

	// WithCanonical forces canonical YAML output format.
	//
	// When enabled, the encoder outputs strictly canonical YAML with explicit
	// tags for all values. This produces verbose output primarily useful for
	// debugging and YAML spec compliance testing.
	// When called without arguments, defaults to true.
	//
	// The default is false.
	WithCanonical = libyaml.WithCanonical

	// WithLineBreak sets the line ending style for YAML output.
	//
	// Available options:
	//   - LineBreakLN: Unix-style \n (default)
	//   - LineBreakCR: Old Mac-style \r
	//   - LineBreakCRLN: Windows-style \r\n
	//
	// The default is LineBreakLN.
	WithLineBreak = libyaml.WithLineBreak

	// WithExplicitStart controls whether document start markers (---) are
	// always emitted.
	//
	// When true, every document begins with an explicit "---" marker.
	// When false (default), the marker is omitted for the first document.
	// When called without arguments, defaults to true.
	WithExplicitStart = libyaml.WithExplicitStart

	// WithExplicitEnd controls whether document end markers (...) are always
	// emitted.
	//
	// When true, every document ends with an explicit "..." marker.
	// When false (default), the marker is omitted.
	// When called without arguments, defaults to true.
	WithExplicitEnd = libyaml.WithExplicitEnd

	// WithFlowSimpleCollections controls whether simple collections use flow
	// style.
	//
	// When true, sequences and mappings containing only scalar values (no
	// nested collections) are rendered in flow style if they fit within the
	// line width.
	// Example: {name: test, count: 42} or [a, b, c]
	// When called without arguments, defaults to true.
	//
	// When false (default), all collections use block style.
	WithFlowSimpleCollections = libyaml.WithFlowSimpleCollections

	// WithQuotePreference sets the preferred quote style for strings that
	// require quoting.
	//
	// This option only affects strings that require quoting per the YAML spec.
	// Plain strings that don't need quoting remain unquoted regardless of this
	// setting. Quoting is required for:
	//   - Strings that look like other YAML types (true, false, null, 123, etc.)
	//   - Strings with leading/trailing whitespace
	//   - Strings containing special YAML syntax characters
	//   - Empty strings in certain contexts
	//
	// Quote styles:
	//   - QuoteSingle: Use single quotes (v4 default)
	//   - QuoteDouble: Use double quotes
	//   - QuoteLegacy: Legacy v2/v3 behavior (mixed quoting)
	WithQuotePreference = libyaml.WithQuotePreference
)

// Options combines multiple options into a single Option.
// This is useful for creating option presets or combining version defaults
// with custom options.
//
// Example:
//
//	opts := yaml.Options(yaml.WithV4Defaults(), yaml.WithIndent(3))
//	yaml.Dump(&data, opts)
func Options(opts ...Option) Option {
	return libyaml.CombineOptions(opts...)
}

// DepthKind represents the type of nesting (flow or block).
type DepthKind = libyaml.DepthKind

// DepthKind constants for nesting depth checks.
const (
	DepthKindFlow  = libyaml.DepthKindFlow
	DepthKindBlock = libyaml.DepthKindBlock
)

// DepthContext holds context about a nesting depth check.
type DepthContext = libyaml.DepthContext

// WithPlugin registers one or more plugins for YAML processing.
//
// Plugins extend the YAML library with custom processing logic.
// Each plugin implements one or more plugin interfaces.
// Currently supported plugin types:
//   - LimitPlugin: Controls depth and alias expansion limits
//   - ParserPlugin: Supplies a complete YAML event stream
//   - JSONCommentsPlugin: Sanitizes JSON-style comments before parsing
//   - TabIndentPlugin: Enables tab-aware loading and tab-indented dumping
//
// Example:
//
//	import "github.com/pb33f/go-yaml/plugin/limit"
//	loader := yaml.NewLoader(data, yaml.WithPlugin(limit.New(limit.AliasNone())))
//
// Plugins use public types and can be implemented by external packages.
func WithPlugin(plugins ...any) Option {
	return func(o *libyaml.Options) error {
		for _, p := range plugins {
			registered := false
			if _, ok := p.(nativeParserPlugin); ok {
				registered = true
			}
			if lp, ok := p.(LimitPlugin); ok {
				o.DepthCheck = lp.CheckDepth
				o.AliasCheck = lp.CheckAlias
				registered = true
			}
			if source, ok := p.(ParserPlugin); ok {
				if o.Parser != nil {
					return errors.New("yaml: multiple parser plugins")
				}
				o.Parser = source
				registered = true
			}
			if comments, ok := p.(JSONCommentsPlugin); ok {
				if o.JSONComments != nil {
					return errors.New(
						"yaml: multiple json-comments plugins")
				}
				o.JSONComments = comments
				registered = true
			}
			if tabs, ok := p.(TabIndentPlugin); ok {
				if o.IndentConfig != nil {
					return errors.New(
						"yaml: multiple tab-indent plugins")
				}
				config, err := tabs.TabIndentConfig().Normalize()
				if err != nil {
					return err
				}
				o.IndentConfig = &config
				registered = true
			}
			if !registered {
				return fmt.Errorf("yaml: unsupported plugin type: %T", p)
			}
		}
		return nil
	}
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
// - plugin (map of plugin API to config)
//
// The plugin field configures plugins by API. Each value is a configuration
// map, true for the default implementation, or false to leave the plugin
// disabled. A map may select an implementation with "name" and require its
// exact release with "version". Versions may include a leading "v".
// A map may contain "disable": true to leave the plugin disabled.
// The "name", "version", and "disable" host fields are not passed to the
// plugin factory. "disable": false keeps the plugin enabled.
// Null plugin values are invalid.
// Currently supported: "limit" with keys "depth" and "alias" (int
// or null to disable).
//
// Only fields specified in the YAML will override other options when
// combined. Unspecified fields won't affect other options.
//
// Example:
//
//	opts, err := yaml.OptsYAML(`
//	  indent: 3
//	  known-fields: true
//	  plugin:
//	    limit:
//	      depth: 50
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
		Plugin                Node    `yaml:"plugin"`
	}
	if err := Load([]byte(yamlStr), &cfg, WithKnownFields()); err != nil {
		return nil, err
	}
	var plugins map[string]any
	switch cfg.Plugin.Kind {
	case 0:
	case MappingNode:
		if err := cfg.Plugin.Load(&plugins); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("yaml: plugin configuration must be a mapping")
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

	for name, val := range plugins {
		var cfgMap map[string]any
		switch v := val.(type) {
		case bool:
			if !v {
				continue
			}
			cfgMap = map[string]any{}
		case map[string]any:
			cfgMap = v
			if raw, found := v["disable"]; found {
				disabled, ok := raw.(bool)
				if !ok {
					return nil, fmt.Errorf(
						"yaml: plugin %q disable must be a boolean", name)
				}
				if disabled {
					continue
				}
				cfgMap = make(map[string]any, len(v)-1)
				for key, value := range v {
					if key != "disable" {
						cfgMap[key] = value
					}
				}
			}
		case string:
			var err error
			cfgMap, err = pluginreg.ConfigValue(name, v)
			if err != nil {
				return nil, fmt.Errorf("yaml: plugin %q: %w", name, err)
			}
		default:
			return nil, fmt.Errorf(
				"yaml: plugin %q value must be a mapping, string, or boolean",
				name)
		}
		p, err := namedPlugin(name, cfgMap)
		if err != nil {
			return nil, err
		}
		optList = append(optList, WithPlugin(p))
	}

	return Options(optList...), nil
}

//-----------------------------------------------------------------------------
// Type and constant re-exports
//-----------------------------------------------------------------------------

// Re-export stream-related types
type (
	Stream           = libyaml.Stream
	VersionDirective = libyaml.StreamVersionDirective

	// TagDirective represents a YAML %TAG directive for stream nodes.
	TagDirective = libyaml.StreamTagDirective

	// Encoding represents the character encoding of a YAML stream.
	Encoding = libyaml.Encoding
)

// Encoding constants for YAML stream encoding
const (
	// EncodingAny lets the parser choose the encoding.
	EncodingAny = libyaml.ANY_ENCODING

	// EncodingUTF8 is the default UTF-8 encoding.
	EncodingUTF8 = libyaml.UTF8_ENCODING

	// EncodingUTF16LE is UTF-16-LE encoding with BOM.
	EncodingUTF16LE = libyaml.UTF16LE_ENCODING

	// EncodingUTF16BE is UTF-16-BE encoding with BOM.
	EncodingUTF16BE = libyaml.UTF16BE_ENCODING
)

// Stage identifies the processing stage where an error occurred during YAML
// loading or dumping.
type Stage = libyaml.Stage

// Stage constants for YAML processing pipeline.
const (
	// Load stages
	ReaderStage      = libyaml.ReaderStage      // Input reading and encoding
	ScannerStage     = libyaml.ScannerStage     // Tokenization
	ParserStage      = libyaml.ParserStage      // Event stream parsing
	ComposerStage    = libyaml.ComposerStage    // Node tree construction
	ResolverStage    = libyaml.ResolverStage    // Tag resolution
	ConstructorStage = libyaml.ConstructorStage // Go value construction

	// Dump stages
	RepresenterStage = libyaml.RepresenterStage // Go value to Node tree
	SerializerStage  = libyaml.SerializerStage  // Node tree to events
	EmitterStage     = libyaml.EmitterStage     // Events to YAML bytes
	WriterStage      = libyaml.WriterStage      // Output writing
)

// Mark represents a position in the YAML document.
type Mark = libyaml.Mark

// Error types for YAML loading and dumping
type (
	// LoadError represents an error encountered while decoding a YAML document.
	//
	// It contains details about the location in the document where the error
	// occurred, as well as the processing stage that generated it.
	LoadError = libyaml.LoadError

	// LoadErrors is returned when one or more fields cannot be properly decoded.
	//
	// It contains multiple *[LoadError] instances with details about each error.
	LoadErrors = libyaml.LoadErrors

	// DumpError represents an error that occurred while dumping a YAML document.
	//
	// It identifies the processing stage where the error occurred and provides
	// an optional underlying cause via Unwrap.
	DumpError = libyaml.DumpError

	// TypeError is a legacy error type retained for compatibility.
	//
	// Deprecated: Use [LoadErrors] instead.
	//
	//nolint:staticcheck // we are using deprecated TypeError for compatibility
	TypeError = libyaml.TypeError
)

// NewLoadError creates a LoadError with an underlying cause error.
// The cause is accessible via Unwrap for use with [errors.Is] and [errors.As].
var NewLoadError = libyaml.NewLoadError

// NewDumpError creates a DumpError with an underlying cause error.
// The cause is accessible via Unwrap for use with [errors.Is] and [errors.As].
var NewDumpError = libyaml.NewDumpError

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
// Load/Dump API
//-----------------------------------------------------------------------------

// Advanced streaming API types

// Loader reads and loads YAML values from an input stream with
// configurable options.
type Loader struct {
	loader *libyaml.Loader
}

// Load reads the next YAML-encoded document from its input and stores it
// in the value pointed to by `out`.
//
// Returns [io.EOF] when there are no more documents to read.
//
// If the [WithSingleDocument] option was set and a document was already
// read, subsequent calls return [io.EOF].
//
// Maps and pointers (to a struct, string, int, etc) are accepted as values
// for `out` values.
//
// If an internal pointer within a struct is not initialized, the yaml
// package will initialize it if necessary for loading the provided
// data.
//
// The `out` parameter must not be nil.
//
// The type of the decoded values should be compatible with the respective
// values in the `out` parameter.
//
// If one or more values cannot be decoded due to a type mismatches,
// decoding continues partially until the end of the YAML content, and
// a [*yaml.LoadErrors] is returned with details for all missed values.
//
// Struct fields are only loaded if they are exported (have an
// upper case first letter), and are loaded using the field name
// lowercased as the default key.
//
// Custom keys may be defined via the `yaml` name in the field `tag:`
// the content preceding the first comma is used as the key, and the
// following comma-separated options are used to tweak the loading
// process (see [Load]). Conflicting names result in a runtime error.
//
// See the documentation of the package-level [Load] function for more details
// about YAML to Go conversion and tag options.
func (l *Loader) Load(out any) error {
	return l.loader.Load(out)
}

// NewLoader returns a new Loader that reads from r with the given options.
//
// The Loader introduces its own buffering and may read data from r beyond the
// YAML values requested.
func NewLoader(r io.Reader, opts ...Option) (*Loader, error) {
	l, err := libyaml.NewLoader(r, opts...)
	if err != nil {
		return nil, err
	}
	return &Loader{
		loader: l,
	}, nil
}

// Load loads YAML document(s) with the given options.
//
// By default, Load requires exactly one document in the input.
// If zero documents are found, it returns an error.
// If multiple documents are found, it returns an error.
//
// Use [WithAllDocuments] to load all documents into a slice:
//
//	var configs []Config
//	yaml.Load(multiDocYAML, &configs, yaml.WithAllDocuments())
//
// When [WithAllDocuments] is used, out must be a pointer to a slice.
// Each document is loaded into the slice element type.
// Zero documents results in an empty slice (no error).
//
// Maps and pointers (to a struct, string, int, etc) are accepted as out
// values.
//
// If an internal pointer within a struct is not initialized, the yaml
// package will initialize it if necessary.
//
// The out parameter must not be nil.
//
// The type of the loaded values should be compatible with the respective
// values in out.
//
// If one or more values cannot be loaded due to type mismatches, decoding
// continues partially until the end of the YAML content, and a
// [*yaml.LoadErrors] is returned with details for all missed values.
//
// Struct fields are only loaded if they are exported (have an upper case
// first letter), and are loaded using the field name lowercased as the
// default key.
//
// Custom keys may be defined via the "yaml" name in the field tag: the
// content preceding the first comma is used as the key, and the
// following comma-separated options control the loading and dumping
// behavior.
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
	return libyaml.Load(in, out, opts...)
}

// Dumper writes YAML values to an output stream with configurable options.
type Dumper struct {
	dumper *libyaml.Dumper
}

// NewDumper returns a new Dumper that writes to w with the given options.
func NewDumper(w io.Writer, opts ...Option) (*Dumper, error) {
	d, err := libyaml.NewDumper(w, opts...)
	if err != nil {
		return nil, err
	}
	return &Dumper{
		dumper: d,
	}, nil
}

// Dump writes the YAML encoding of v to the stream.
//
// If multiple values are dumped to the stream, the second and subsequent
// documents will be preceded with a "---" document separator.
//
// See the documentation for [Marshal] for details about the conversion of Go
// values to YAML.
func (d *Dumper) Dump(in any) error {
	return d.dumper.Dump(in)
}

// Close closes the Dumper by writing any remaining data.
// It does not write a stream terminating string "...".
func (d *Dumper) Close() error {
	return d.dumper.Close()
}

// Dump encodes a value to YAML with the given options.
//
// By default, Dump encodes a single value as a single YAML document.
//
// Use [WithAllDocuments] to encode multiple values as a multi-document stream:
//
//	docs := []Config{config1, config2, config3}
//	yaml.Dump(docs, yaml.WithAllDocuments())
//
// When [WithAllDocuments] is used, in must be a slice.
// Each element is encoded as a separate YAML document with "---" separators.
//
// See [Marshal] for details.
func Dump(in any, opts ...Option) (out []byte, err error) {
	return libyaml.Dump(in, opts...)
}

//-----------------------------------------------------------------------------
// Classic APIs
//-----------------------------------------------------------------------------

// A Decoder reads and decodes YAML values from an input stream.
type Decoder struct {
	loader *libyaml.Loader
}

// NewDecoder returns a new decoder that reads from r.
//
// The decoder introduces its own buffering and may read
// data from r beyond the YAML values requested.
func NewDecoder(r io.Reader) *Decoder {
	// NewLoader won't return error with WithV3Defaults() and withFromLegacy
	loader, _ := libyaml.NewLoader(r, WithV3Defaults(), withFromLegacy())
	return &Decoder{loader: loader}
}

// KnownFields ensures that the keys in decoded mappings to
// exist as fields in the struct being decoded into.
func (dec *Decoder) KnownFields(enable bool) {
	dec.loader.SetLegacyLoaderKnownFields(enable)
}

// Decode reads the next YAML-encoded value from its input
// and stores it in the value pointed to by v.
//
// See the documentation for Unmarshal for details about the
// conversion of YAML into a Go value.
func (dec *Decoder) Decode(v any) error {
	return dec.loader.Load(v)
}

// An Encoder writes YAML values to an output stream.
type Encoder struct {
	dumper *libyaml.Dumper
}

// NewEncoder returns a new encoder that writes to w.
// The Encoder should be closed after use to flush all data
// to w.
func NewEncoder(w io.Writer) *Encoder {
	// NewDumper won't return an error when using WithV3Defaults()
	dumper, _ := libyaml.NewDumper(w, WithV3Defaults())
	return &Encoder{dumper: dumper}
}

// Encode writes the YAML encoding of v to the stream.
// If multiple items are encoded to the stream, the
// second and subsequent document will be preceded
// with a "---" document separator, but the first will not.
//
// See the documentation for Marshal for details about the conversion of Go
// values to YAML.
func (e *Encoder) Encode(v any) error {
	return e.dumper.Dump(v)
}

// SetIndent changes the used indentation used when encoding.
func (e *Encoder) SetIndent(spaces int) {
	e.dumper.SetLegacyEncoderIndent(spaces)
}

// CompactSeqIndent makes it so that '- ' is considered part of the indentation.
func (e *Encoder) CompactSeqIndent() {
	e.dumper.SetLegacyEncoderCompactSeqIndent(true)
}

// DefaultSeqIndent makes it so that '- ' is not considered part of the indentation.
func (e *Encoder) DefaultSeqIndent() {
	e.dumper.SetLegacyEncoderCompactSeqIndent(false)
}

// Close closes the encoder by writing any remaining data.
// It does not write a stream terminating string "...".
func (e *Encoder) Close() error {
	return e.dumper.Close()
}

// Unmarshal decodes the *first* document found within the in byte slice
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
//	yaml.Unmarshal([]byte("a: 1\nb: 2"), &t)
//
// See the documentation of Marshal for the format of tags and a list of
// supported tag options.
func Unmarshal(in []byte, out any) (err error) {
	return Load(in, out, WithV3Defaults(), withFromLegacy())
}

// withFromLegacy is a private option that indicates this call is from
// a legacy API (Unmarshal/Decoder). It enables Unmarshaler interface
// checking and allows trailing content for backward compatibility.
func withFromLegacy() Option {
	return func(o *libyaml.Options) error {
		libyaml.SetLegacyAllowTrailingContent(o)
		return nil
	}
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
	// Use WithV3Defaults() with unlimited line width to match legacy DefaultOptions
	return Dump(in, WithV3Defaults(), WithLineWidth(-1))
}
