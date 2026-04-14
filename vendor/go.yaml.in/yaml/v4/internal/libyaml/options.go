//
// Copyright (c) 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0
//

// Options configuration for loading and dumping YAML.
// Provides centralized control for indentation, line width, strictness, and
// more.

package libyaml

import (
	"errors"
	"fmt"
)

// Options holds configuration for both loading and dumping YAML.
type Options struct {
	// Loading options
	KnownFields    bool // Enforce known fields in structs
	SingleDocument bool // Only load first document
	UniqueKeys     bool // Enforce unique keys in mappings
	StreamNodes    bool // Enable stream node emission
	AllDocuments   bool // Load/Dump all documents in multi-document streams

	// Dumping options
	Indent                int        // Indentation spaces (2-9)
	CompactSeqIndent      bool       // Whether '- ' counts as indentation
	LineWidth             int        // Preferred line width (-1 for unlimited)
	Unicode               bool       // Allow non-ASCII characters
	Canonical             bool       // Canonical YAML output
	LineBreak             LineBreak  // Line ending style
	ExplicitStart         bool       // Always emit ---
	ExplicitEnd           bool       // Always emit ...
	FlowSimpleCollections bool       // Use flow style for simple collections
	QuotePreference       QuoteStyle // Preferred quote style when quoting is required
}

// Option allows configuring YAML loading and dumping operations.
type Option func(*Options) error

// WithIndent sets the number of spaces to use for indentation when
// dumping YAML content.
//
// Valid values are 2-9. Common choices: 2 (compact), 4 (readable).
func WithIndent(indent int) Option {
	return func(o *Options) error {
		if indent < 2 || indent > 9 {
			return errors.New("yaml: indent must be between 2 and 9 spaces")
		}
		o.Indent = indent
		return nil
	}
}

// WithCompactSeqIndent configures whether the sequence indicator '- ' is
// considered part of the indentation when dumping YAML content.
//
// If compact is true, '- ' is treated as part of the indentation.
// If compact is false, '- ' is not treated as part of the indentation.
// When called without arguments, defaults to true.
func WithCompactSeqIndent(compact ...bool) Option {
	if len(compact) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithCompactSeqIndent accepts at most one argument")
		}
	}
	val := len(compact) == 0 || compact[0]
	return func(o *Options) error {
		o.CompactSeqIndent = val
		return nil
	}
}

// WithKnownFields enables or disables strict field checking during YAML loading.
//
// When enabled, loading will return an error if the YAML input contains fields
// that do not correspond to any fields in the target struct.
// When called without arguments, defaults to true.
func WithKnownFields(knownFields ...bool) Option {
	if len(knownFields) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithKnownFields accepts at most one argument")
		}
	}
	val := len(knownFields) == 0 || knownFields[0]
	return func(o *Options) error {
		o.KnownFields = val
		return nil
	}
}

// WithSingleDocument configures the Loader to only process the first document
// in a YAML stream. After the first document is loaded, subsequent calls to
// Load will return io.EOF.
//
// When called without arguments, defaults to true.
//
// This is useful when you expect exactly one document and want behavior
// similar to [Unmarshal].
func WithSingleDocument(singleDocument ...bool) Option {
	if len(singleDocument) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithSingleDocument accepts at most one argument")
		}
	}
	val := len(singleDocument) == 0 || singleDocument[0]
	return func(o *Options) error {
		o.SingleDocument = val
		return nil
	}
}

// WithStreamNodes enables returning stream boundary nodes when loading YAML.
//
// When enabled, Loader.Load returns an interleaved sequence of StreamNode and
// DocumentNode values:
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
func WithStreamNodes(enable ...bool) Option {
	if len(enable) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithStreamNodes accepts at most one argument")
		}
	}
	val := len(enable) == 0 || enable[0]
	return func(o *Options) error {
		o.StreamNodes = val
		return nil
	}
}

// WithAllDocuments enables multi-document mode for Load and Dump operations.
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
func WithAllDocuments(all ...bool) Option {
	if len(all) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithAllDocuments accepts at most one argument")
		}
	}
	val := len(all) == 0 || all[0]
	return func(o *Options) error {
		o.AllDocuments = val
		return nil
	}
}

// WithLineWidth sets the preferred line width for YAML output.
//
// When encoding long strings, the encoder will attempt to wrap them at this
// width using literal block style (|). Set to -1 or 0 for unlimited width.
//
// The default is 80 characters.
func WithLineWidth(width int) Option {
	return func(o *Options) error {
		if width < 0 {
			width = -1
		}
		o.LineWidth = width
		return nil
	}
}

// WithUnicode controls whether non-ASCII characters are allowed in YAML output.
//
// When true, non-ASCII characters appear as-is (e.g., "café").
// When false, non-ASCII characters are escaped (e.g., "caf\u00e9").
// When called without arguments, defaults to true.
//
// The default is true.
func WithUnicode(unicode ...bool) Option {
	if len(unicode) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithUnicode accepts at most one argument")
		}
	}
	val := len(unicode) == 0 || unicode[0]
	return func(o *Options) error {
		o.Unicode = val
		return nil
	}
}

// WithUniqueKeys enables or disables duplicate key detection during YAML loading.
//
// When enabled, loading will return an error if the YAML input contains
// duplicate keys in any mapping. This is a security feature that prevents
// key override attacks.
// When called without arguments, defaults to true.
//
// The default is true.
func WithUniqueKeys(uniqueKeys ...bool) Option {
	if len(uniqueKeys) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithUniqueKeys accepts at most one argument")
		}
	}
	val := len(uniqueKeys) == 0 || uniqueKeys[0]
	return func(o *Options) error {
		o.UniqueKeys = val
		return nil
	}
}

// WithCanonical forces canonical YAML output format.
//
// When enabled, the encoder outputs strictly canonical YAML with explicit
// tags for all values. This produces verbose output primarily useful for
// debugging and YAML spec compliance testing.
// When called without arguments, defaults to true.
//
// The default is false.
func WithCanonical(canonical ...bool) Option {
	if len(canonical) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithCanonical accepts at most one argument")
		}
	}
	val := len(canonical) == 0 || canonical[0]
	return func(o *Options) error {
		o.Canonical = val
		return nil
	}
}

// WithLineBreak sets the line ending style for YAML output.
//
// Available options:
//   - LineBreakLN: Unix-style \n (default)
//   - LineBreakCR: Old Mac-style \r
//   - LineBreakCRLN: Windows-style \r\n
//
// The default is LineBreakLN.
func WithLineBreak(lineBreak LineBreak) Option {
	return func(o *Options) error {
		o.LineBreak = lineBreak
		return nil
	}
}

// WithExplicitStart controls whether document start markers (---) are always emitted.
//
// When true, every document begins with an explicit "---" marker.
// When false (default), the marker is omitted for the first document.
// When called without arguments, defaults to true.
func WithExplicitStart(explicit ...bool) Option {
	if len(explicit) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithExplicitStart accepts at most one argument")
		}
	}
	val := len(explicit) == 0 || explicit[0]
	return func(o *Options) error {
		o.ExplicitStart = val
		return nil
	}
}

// WithExplicitEnd controls whether document end markers (...) are always emitted.
//
// When true, every document ends with an explicit "..." marker.
// When false (default), the marker is omitted.
// When called without arguments, defaults to true.
func WithExplicitEnd(explicit ...bool) Option {
	if len(explicit) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithExplicitEnd accepts at most one argument")
		}
	}
	val := len(explicit) == 0 || explicit[0]
	return func(o *Options) error {
		o.ExplicitEnd = val
		return nil
	}
}

// WithFlowSimpleCollections controls whether simple collections use flow style.
//
// When true, sequences and mappings containing only scalar values (no nested
// collections) are rendered in flow style if they fit within the line width.
// Example: {name: test, count: 42} or [a, b, c]
// When called without arguments, defaults to true.
//
// When false (default), all collections use block style.
func WithFlowSimpleCollections(flow ...bool) Option {
	if len(flow) > 1 {
		return func(o *Options) error {
			return errors.New("yaml: WithFlowSimpleCollections accepts at most one argument")
		}
	}
	val := len(flow) == 0 || flow[0]
	return func(o *Options) error {
		o.FlowSimpleCollections = val
		return nil
	}
}

// WithQuotePreference sets the preferred quote style for strings that require
// quoting.
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
func WithQuotePreference(style QuoteStyle) Option {
	return func(o *Options) error {
		switch style {
		case QuoteSingle, QuoteDouble, QuoteLegacy:
			o.QuotePreference = style
			return nil
		default:
			return fmt.Errorf("invalid QuoteStyle value: %d", style)
		}
	}
}

// CombineOptions combines multiple options into a single Option.
// This is useful for creating option presets or combining version defaults
// with custom options.
func CombineOptions(opts ...Option) Option {
	return func(o *Options) error {
		for _, opt := range opts {
			if err := opt(o); err != nil {
				return err
			}
		}
		return nil
	}
}

// ApplyOptions applies the given options to a new options struct.
// Starts with v4 defaults.
func ApplyOptions(opts ...Option) (*Options, error) {
	o := &Options{
		Canonical: false,
		LineBreak: LN_BREAK,

		// v4 defaults
		Indent:           2,
		CompactSeqIndent: true,
		LineWidth:        80,
		Unicode:          true,
		UniqueKeys:       true,
	}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// DefaultOptions holds the default options for APIs that don't accept options.
var DefaultOptions = &Options{
	Indent:          4,
	LineWidth:       -1,
	Unicode:         true,
	UniqueKeys:      true,
	QuotePreference: QuoteLegacy,
}
