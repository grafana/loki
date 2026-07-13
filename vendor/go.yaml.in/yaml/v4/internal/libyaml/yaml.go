// Copyright 2006-2010 Kirill Simonov
// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0 AND MIT

// Core libyaml types and structures.
// Defines Parser, Emitter, Event, Token, and related constants for YAML
// processing.

package libyaml

import (
	"fmt"
	"strings"
)

// VersionDirective holds the YAML version directive data.
type VersionDirective struct {
	major int8 // The major version number.
	minor int8 // The minor version number.
}

// Major returns the major version number.
func (v *VersionDirective) Major() int { return int(v.major) }

// Minor returns the minor version number.
func (v *VersionDirective) Minor() int { return int(v.minor) }

// TagDirective holds the YAML tag directive data.
type TagDirective struct {
	handle []byte // The tag handle.
	prefix []byte // The tag prefix.
}

// GetHandle returns the tag handle.
func (t *TagDirective) GetHandle() string { return string(t.handle) }

// GetPrefix returns the tag prefix.
func (t *TagDirective) GetPrefix() string { return string(t.prefix) }

// Encoding represents the character encoding of a YAML stream.
type Encoding int

// The stream encoding.
const (
	// Let the parser choose the encoding.
	ANY_ENCODING Encoding = iota

	UTF8_ENCODING    // The default UTF-8 encoding.
	UTF16LE_ENCODING // The UTF-16-LE encoding with BOM.
	UTF16BE_ENCODING // The UTF-16-BE encoding with BOM.
)

// LineBreak represents the line break style used in YAML output.
type LineBreak int

// Line break types.
const (
	// Let the parser choose the break type.
	ANY_BREAK LineBreak = iota

	CR_BREAK   // Use CR for line breaks (Mac style).
	LN_BREAK   // Use LN for line breaks (Unix style).
	CRLN_BREAK // Use CR LN for line breaks (DOS style).
)

// QuoteStyle represents the preferred quote style for scalar values.
type QuoteStyle int

// Quote style types for required quoting.
const (
	QuoteSingle QuoteStyle = iota // Prefer single quotes when quoting is required.
	QuoteDouble                   // Prefer double quotes when quoting is required.
	QuoteLegacy                   // Legacy behavior: double in representer, single in emitter.
)

// ScalarStyle returns the scalar style for this quote preference in the
// representer/serializer context.
// In this context, both QuoteDouble and QuoteLegacy use double quotes.
func (q QuoteStyle) ScalarStyle() ScalarStyle {
	if q == QuoteDouble || q == QuoteLegacy {
		return DOUBLE_QUOTED_SCALAR_STYLE
	}
	return SINGLE_QUOTED_SCALAR_STYLE
}

// ErrorType represents the category of error that occurred during processing.
type ErrorType int

// Many bad things could happen with the parser and emitter.
const (
	// No error is produced.
	NO_ERROR ErrorType = iota

	MEMORY_ERROR   // Cannot allocate or reallocate a block of memory.
	READER_ERROR   // Cannot read or decode the input stream.
	SCANNER_ERROR  // Cannot scan the input stream.
	PARSER_ERROR   // Cannot parse the input stream.
	COMPOSER_ERROR // Cannot compose a YAML document.
	WRITER_ERROR   // Cannot write to the output stream.
	EMITTER_ERROR  // Cannot emit a YAML stream.
)

// Mark holds the pointer position.
type Mark struct {
	Index  int // The position index.
	Line   int // The position line (1-indexed; 0 means unknown).
	Column int // The position column (1-indexed; 0 means unknown).
}

// String returns a human-readable string representation of the position mark.
func (m Mark) String() string {
	var builder strings.Builder
	if m.Line == 0 {
		return "<unknown position>"
	}

	fmt.Fprintf(&builder, "line %d", m.Line)
	if m.Column > 0 {
		fmt.Fprintf(&builder, ", column %d", m.Column)
	}

	return builder.String()
}

// shortString returns a compact position string.
// Returns "<unknown position>" when Line is 0 (position not known).
// When Column is 0 (unknown), it is omitted from output ("L{line}");
// otherwise it is displayed as "L{line}.C{col}".
func (m Mark) shortString() string {
	if m.Line == 0 {
		return "<unknown position>"
	}
	if m.Column > 0 {
		return fmt.Sprintf("L%d.C%d", m.Line, m.Column)
	}
	return fmt.Sprintf("L%d", m.Line)
}

// rangeString formats a position range from start mark m to end mark.
// Both marks use shortString for their individual display.
// When marks are on the same line:
//   - Both Column==0: just "L2" (unknown columns, no range shown)
//   - Both Column>0: "L2.C6-C7" (compact column range)
//   - Mixed columns: "L1.C4-L1" (full start with line-only end)
//
// When marks are on different lines: "L1.C8-L2.C3"
func (m Mark) rangeString(end Mark) string {
	start := m.shortString()
	if m.Line == end.Line {
		if m.Column == 0 && end.Column == 0 {
			// Same line, unknown columns: just "L2"
			return start
		}
		if m.Column > 0 && end.Column > 0 {
			if m.Column == end.Column {
				// Same position: just "L2.C6"
				return start
			}
			// Same line with columns: "L2.C6-C7"
			return fmt.Sprintf("%s-C%d", start, end.Column)
		}
	}
	return fmt.Sprintf("%s-%s", start, end.shortString())
}

// Node Styles

// styleInt is the underlying type for style constants.
type styleInt int8

// ScalarStyle represents the formatting style of a scalar value.
type ScalarStyle styleInt

// Scalar styles.
const (
	// Let the emitter choose the style.
	ANY_SCALAR_STYLE ScalarStyle = 0

	PLAIN_SCALAR_STYLE         ScalarStyle = 1 << iota // The plain scalar style.
	SINGLE_QUOTED_SCALAR_STYLE                         // The single-quoted scalar style.
	DOUBLE_QUOTED_SCALAR_STYLE                         // The double-quoted scalar style.
	LITERAL_SCALAR_STYLE                               // The literal scalar style.
	FOLDED_SCALAR_STYLE                                // The folded scalar style.
)

// String returns a string representation of a [ScalarStyle].
func (style ScalarStyle) String() string {
	switch style {
	case PLAIN_SCALAR_STYLE:
		return "Plain"
	case SINGLE_QUOTED_SCALAR_STYLE:
		return "Single"
	case DOUBLE_QUOTED_SCALAR_STYLE:
		return "Double"
	case LITERAL_SCALAR_STYLE:
		return "Literal"
	case FOLDED_SCALAR_STYLE:
		return "Folded"
	default:
		return ""
	}
}

// SequenceStyle represents the formatting style of a sequence node.
type SequenceStyle styleInt

// Sequence styles.
const (
	// Let the emitter choose the style.
	ANY_SEQUENCE_STYLE SequenceStyle = iota

	BLOCK_SEQUENCE_STYLE // The block sequence style.
	FLOW_SEQUENCE_STYLE  // The flow sequence style.
)

// MappingStyle represents the formatting style of a mapping node.
type MappingStyle styleInt

// Mapping styles.
const (
	// Let the emitter choose the style.
	ANY_MAPPING_STYLE MappingStyle = iota

	BLOCK_MAPPING_STYLE // The block mapping style.
	FLOW_MAPPING_STYLE  // The flow mapping style.
)

// Tokens

// TokenType represents the type of a scanned token.
type TokenType int

// Token types.
const (
	// An empty token.
	NO_TOKEN TokenType = iota

	STREAM_START_TOKEN // A STREAM-START token.
	STREAM_END_TOKEN   // A STREAM-END token.

	VERSION_DIRECTIVE_TOKEN // A VERSION-DIRECTIVE token.
	TAG_DIRECTIVE_TOKEN     // A TAG-DIRECTIVE token.
	DOCUMENT_START_TOKEN    // A DOCUMENT-START token.
	DOCUMENT_END_TOKEN      // A DOCUMENT-END token.

	BLOCK_SEQUENCE_START_TOKEN // A BLOCK-SEQUENCE-START token.
	BLOCK_MAPPING_START_TOKEN  // A BLOCK-SEQUENCE-END token.
	BLOCK_END_TOKEN            // A BLOCK-END token.

	FLOW_SEQUENCE_START_TOKEN // A FLOW-SEQUENCE-START token.
	FLOW_SEQUENCE_END_TOKEN   // A FLOW-SEQUENCE-END token.
	FLOW_MAPPING_START_TOKEN  // A FLOW-MAPPING-START token.
	FLOW_MAPPING_END_TOKEN    // A FLOW-MAPPING-END token.

	BLOCK_ENTRY_TOKEN // A BLOCK-ENTRY token.
	FLOW_ENTRY_TOKEN  // A FLOW-ENTRY token.
	KEY_TOKEN         // A KEY token.
	VALUE_TOKEN       // A VALUE token.

	ALIAS_TOKEN   // An ALIAS token.
	ANCHOR_TOKEN  // An ANCHOR token.
	TAG_TOKEN     // A TAG token.
	SCALAR_TOKEN  // A SCALAR token.
	COMMENT_TOKEN // A COMMENT token.
)

// String returns a string representation of the token type.
func (tt TokenType) String() string {
	switch tt {
	case NO_TOKEN:
		return "NO_TOKEN"
	case STREAM_START_TOKEN:
		return "STREAM_START_TOKEN"
	case STREAM_END_TOKEN:
		return "STREAM_END_TOKEN"
	case VERSION_DIRECTIVE_TOKEN:
		return "VERSION_DIRECTIVE_TOKEN"
	case TAG_DIRECTIVE_TOKEN:
		return "TAG_DIRECTIVE_TOKEN"
	case DOCUMENT_START_TOKEN:
		return "DOCUMENT_START_TOKEN"
	case DOCUMENT_END_TOKEN:
		return "DOCUMENT_END_TOKEN"
	case BLOCK_SEQUENCE_START_TOKEN:
		return "BLOCK_SEQUENCE_START_TOKEN"
	case BLOCK_MAPPING_START_TOKEN:
		return "BLOCK_MAPPING_START_TOKEN"
	case BLOCK_END_TOKEN:
		return "BLOCK_END_TOKEN"
	case FLOW_SEQUENCE_START_TOKEN:
		return "FLOW_SEQUENCE_START_TOKEN"
	case FLOW_SEQUENCE_END_TOKEN:
		return "FLOW_SEQUENCE_END_TOKEN"
	case FLOW_MAPPING_START_TOKEN:
		return "FLOW_MAPPING_START_TOKEN"
	case FLOW_MAPPING_END_TOKEN:
		return "FLOW_MAPPING_END_TOKEN"
	case BLOCK_ENTRY_TOKEN:
		return "BLOCK_ENTRY_TOKEN"
	case FLOW_ENTRY_TOKEN:
		return "FLOW_ENTRY_TOKEN"
	case KEY_TOKEN:
		return "KEY_TOKEN"
	case VALUE_TOKEN:
		return "VALUE_TOKEN"
	case ALIAS_TOKEN:
		return "ALIAS_TOKEN"
	case ANCHOR_TOKEN:
		return "ANCHOR_TOKEN"
	case TAG_TOKEN:
		return "TAG_TOKEN"
	case SCALAR_TOKEN:
		return "SCALAR_TOKEN"
	case COMMENT_TOKEN:
		return "COMMENT_TOKEN"
	}
	return "<unknown token>"
}

// Token holds information about a scanning token.
type Token struct {
	// The token type.
	Type TokenType

	// The start/end of the token.
	StartMark, EndMark Mark

	// The stream encoding (for STREAM_START_TOKEN).
	encoding Encoding

	// The alias/anchor/scalar Value or tag/tag directive handle
	// (for ALIAS_TOKEN, ANCHOR_TOKEN, SCALAR_TOKEN, TAG_TOKEN, TAG_DIRECTIVE_TOKEN).
	Value []byte

	// The tag suffix (for TAG_TOKEN).
	suffix []byte

	// The tag directive prefix (for TAG_DIRECTIVE_TOKEN).
	prefix []byte

	// The scalar Style (for SCALAR_TOKEN).
	Style ScalarStyle

	// The version directive major/minor (for VERSION_DIRECTIVE_TOKEN).
	major, minor int8
}

// Events

// EventType represents the type of a parsing or emitting event.
type EventType int8

// Event types.
const (
	// An empty event.
	NO_EVENT EventType = iota

	STREAM_START_EVENT   // A STREAM-START event.
	STREAM_END_EVENT     // A STREAM-END event.
	DOCUMENT_START_EVENT // A DOCUMENT-START event.
	DOCUMENT_END_EVENT   // A DOCUMENT-END event.
	ALIAS_EVENT          // An ALIAS event.
	SCALAR_EVENT         // A SCALAR event.
	SEQUENCE_START_EVENT // A SEQUENCE-START event.
	SEQUENCE_END_EVENT   // A SEQUENCE-END event.
	MAPPING_START_EVENT  // A MAPPING-START event.
	MAPPING_END_EVENT    // A MAPPING-END event.
	TAIL_COMMENT_EVENT
)

// eventStrings maps EventType constants to their string representations.
var eventStrings = []string{
	NO_EVENT:             "none",
	STREAM_START_EVENT:   "stream start",
	STREAM_END_EVENT:     "stream end",
	DOCUMENT_START_EVENT: "document start",
	DOCUMENT_END_EVENT:   "document end",
	ALIAS_EVENT:          "alias",
	SCALAR_EVENT:         "scalar",
	SEQUENCE_START_EVENT: "sequence start",
	SEQUENCE_END_EVENT:   "sequence end",
	MAPPING_START_EVENT:  "mapping start",
	MAPPING_END_EVENT:    "mapping end",
	TAIL_COMMENT_EVENT:   "tail comment",
}

// String returns a string representation of the event type.
func (e EventType) String() string {
	if e < 0 || int(e) >= len(eventStrings) {
		return fmt.Sprintf("unknown event %d", e)
	}
	return eventStrings[e]
}

// Event holds information about a parsing or emitting event.
type Event struct {
	// The event type.
	Type EventType

	// The start and end of the event.
	StartMark, EndMark Mark

	// The document encoding (for STREAM_START_EVENT).
	encoding Encoding

	// The version directive (for DOCUMENT_START_EVENT).
	versionDirective *VersionDirective

	// The list of tag directives (for DOCUMENT_START_EVENT).
	tagDirectives []TagDirective

	// The comments
	HeadComment []byte
	LineComment []byte
	FootComment []byte
	TailComment []byte

	// The Anchor (for SCALAR_EVENT, SEQUENCE_START_EVENT, MAPPING_START_EVENT, ALIAS_EVENT).
	Anchor []byte

	// The Tag (for SCALAR_EVENT, SEQUENCE_START_EVENT, MAPPING_START_EVENT).
	Tag []byte

	// The scalar Value (for SCALAR_EVENT).
	Value []byte

	// Is the document start/end indicator Implicit, or the tag optional?
	// (for DOCUMENT_START_EVENT, DOCUMENT_END_EVENT, SEQUENCE_START_EVENT, MAPPING_START_EVENT, SCALAR_EVENT).
	Implicit bool

	// Is the tag optional for any non-plain style? (for SCALAR_EVENT).
	quoted_implicit bool

	// The Style (for SCALAR_EVENT, SEQUENCE_START_EVENT, MAPPING_START_EVENT).
	Style Style
}

// ScalarStyle returns the style of a scalar event.
func (e *Event) ScalarStyle() ScalarStyle { return ScalarStyle(e.Style) }

// SequenceStyle returns the style of a sequence event.
func (e *Event) SequenceStyle() SequenceStyle { return SequenceStyle(e.Style) }

// MappingStyle returns the style of a mapping event.
func (e *Event) MappingStyle() MappingStyle { return MappingStyle(e.Style) }

// GetEncoding returns the stream encoding (for STREAM_START_EVENT).
func (e *Event) GetEncoding() Encoding { return e.encoding }

// GetVersionDirective returns the version directive (for DOCUMENT_START_EVENT).
func (e *Event) GetVersionDirective() *VersionDirective { return e.versionDirective }

// GetTagDirectives returns the tag directives (for DOCUMENT_START_EVENT).
func (e *Event) GetTagDirectives() []TagDirective { return e.tagDirectives }

// Nodes
const (
	NULL_TAG      = "tag:yaml.org,2002:null"      // The tag !!null with the only possible value: null.
	BOOL_TAG      = "tag:yaml.org,2002:bool"      // The tag !!bool with the values: true and false.
	STR_TAG       = "tag:yaml.org,2002:str"       // The tag !!str for string values.
	INT_TAG       = "tag:yaml.org,2002:int"       // The tag !!int for integer values.
	FLOAT_TAG     = "tag:yaml.org,2002:float"     // The tag !!float for float values.
	TIMESTAMP_TAG = "tag:yaml.org,2002:timestamp" // The tag !!timestamp for date and time values.

	SEQ_TAG = "tag:yaml.org,2002:seq" // The tag !!seq is used to denote sequences.
	MAP_TAG = "tag:yaml.org,2002:map" // The tag !!map is used to denote mapping.

	// Not in original libyaml.
	BINARY_TAG = "tag:yaml.org,2002:binary"
	MERGE_TAG  = "tag:yaml.org,2002:merge"

	DEFAULT_SCALAR_TAG   = STR_TAG // The default scalar tag is !!str.
	DEFAULT_SEQUENCE_TAG = SEQ_TAG // The default sequence tag is !!seq.
	DEFAULT_MAPPING_TAG  = MAP_TAG // The default mapping tag is !!map.
)
