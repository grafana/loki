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
	"io"
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

type Encoding int

// The stream encoding.
const (
	// Let the parser choose the encoding.
	ANY_ENCODING Encoding = iota

	UTF8_ENCODING    // The default UTF-8 encoding.
	UTF16LE_ENCODING // The UTF-16-LE encoding with BOM.
	UTF16BE_ENCODING // The UTF-16-BE encoding with BOM.
)

type LineBreak int

// Line break types.
const (
	// Let the parser choose the break type.
	ANY_BREAK LineBreak = iota

	CR_BREAK   // Use CR for line breaks (Mac style).
	LN_BREAK   // Use LN for line breaks (Unix style).
	CRLN_BREAK // Use CR LN for line breaks (DOS style).
)

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
	Line   int // The position line (1-indexed).
	Column int // The position column (0-indexed internally, displayed as 1-indexed).
}

func (m Mark) String() string {
	var builder strings.Builder
	if m.Line == 0 {
		return "<unknown position>"
	}

	fmt.Fprintf(&builder, "line %d", m.Line)
	if m.Column != 0 {
		fmt.Fprintf(&builder, ", column %d", m.Column+1)
	}

	return builder.String()
}

// Node Styles

type styleInt int8

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

type SequenceStyle styleInt

// Sequence styles.
const (
	// Let the emitter choose the style.
	ANY_SEQUENCE_STYLE SequenceStyle = iota

	BLOCK_SEQUENCE_STYLE // The block sequence style.
	FLOW_SEQUENCE_STYLE  // The flow sequence style.
)

type MappingStyle styleInt

// Mapping styles.
const (
	// Let the emitter choose the style.
	ANY_MAPPING_STYLE MappingStyle = iota

	BLOCK_MAPPING_STYLE // The block mapping style.
	FLOW_MAPPING_STYLE  // The flow mapping style.
)

// Tokens

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

func (e *Event) ScalarStyle() ScalarStyle     { return ScalarStyle(e.Style) }
func (e *Event) SequenceStyle() SequenceStyle { return SequenceStyle(e.Style) }
func (e *Event) MappingStyle() MappingStyle   { return MappingStyle(e.Style) }

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

type NodeType int

// Node types.
const (
	// An empty node.
	NO_NODE NodeType = iota

	SCALAR_NODE   // A scalar node.
	SEQUENCE_NODE // A sequence node.
	MAPPING_NODE  // A mapping node.
)

// NodeItem represents an element of a sequence node.
type NodeItem int

// NodePair represents an element of a mapping node.
type NodePair struct {
	key   int // The key of the element.
	value int // The value of the element.
}

// parserNode represents a single node in the YAML document tree.
type parserNode struct {
	typ NodeType // The node type.
	tag []byte   // The node tag.

	// The node data.

	// The scalar parameters (for SCALAR_NODE).
	scalar struct {
		value  []byte      // The scalar value.
		length int         // The length of the scalar value.
		style  ScalarStyle // The scalar style.
	}

	// The sequence parameters (for YAML_SEQUENCE_NODE).
	sequence struct {
		items_data []NodeItem    // The stack of sequence items.
		style      SequenceStyle // The sequence style.
	}

	// The mapping parameters (for MAPPING_NODE).
	mapping struct {
		pairs_data  []NodePair   // The stack of mapping pairs (key, value).
		pairs_start *NodePair    // The beginning of the stack.
		pairs_end   *NodePair    // The end of the stack.
		pairs_top   *NodePair    // The top of the stack.
		style       MappingStyle // The mapping style.
	}

	start_mark Mark // The beginning of the node.
	end_mark   Mark // The end of the node.
}

// Document structure.
type Document struct {
	// The document nodes.
	nodes []parserNode

	// The version directive.
	version_directive *VersionDirective

	// The list of tag directives.
	tag_directives_data  []TagDirective
	tag_directives_start int // The beginning of the tag directives list.
	tag_directives_end   int // The end of the tag directives list.

	start_implicit int // Is the document start indicator implicit?
	end_implicit   int // Is the document end indicator implicit?

	// The start/end of the document.
	start_mark, end_mark Mark
}

// ReadHandler is called when the [Parser] needs to read more bytes from the
// source. The handler should write not more than size bytes to the buffer.
// The number of written bytes should be set to the size_read variable.
//
// [in,out]   data        A pointer to an application data specified by
//
//	yamlParser.setInput().
//
// [out]      buffer      The buffer to write the data from the source.
// [in]       size        The size of the buffer.
// [out]      size_read   The actual number of bytes read from the source.
//
// On success, the handler should return 1.  If the handler failed,
// the returned value should be 0. On EOF, the handler should set the
// size_read to 0 and return 1.
type ReadHandler func(parser *Parser, buffer []byte) (n int, err error)

// SimpleKey holds information about a potential simple key.
type SimpleKey struct {
	flow_level   int  // What flow level is the key at?
	required     bool // Is a simple key required?
	token_number int  // The number of the token.
	mark         Mark // The position mark.
}

// ParserState represents the state of the parser.
type ParserState int

const (
	PARSE_STREAM_START_STATE ParserState = iota

	PARSE_IMPLICIT_DOCUMENT_START_STATE           // Expect the beginning of an implicit document.
	PARSE_DOCUMENT_START_STATE                    // Expect DOCUMENT-START.
	PARSE_DOCUMENT_CONTENT_STATE                  // Expect the content of a document.
	PARSE_DOCUMENT_END_STATE                      // Expect DOCUMENT-END.
	PARSE_BLOCK_NODE_STATE                        // Expect a block node.
	PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE        // Expect the first entry of a block sequence.
	PARSE_BLOCK_SEQUENCE_ENTRY_STATE              // Expect an entry of a block sequence.
	PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE         // Expect an entry of an indentless sequence.
	PARSE_BLOCK_MAPPING_FIRST_KEY_STATE           // Expect the first key of a block mapping.
	PARSE_BLOCK_MAPPING_KEY_STATE                 // Expect a block mapping key.
	PARSE_BLOCK_MAPPING_VALUE_STATE               // Expect a block mapping value.
	PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE         // Expect the first entry of a flow sequence.
	PARSE_FLOW_SEQUENCE_ENTRY_STATE               // Expect an entry of a flow sequence.
	PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE   // Expect a key of an ordered mapping.
	PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE // Expect a value of an ordered mapping.
	PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE   // Expect the and of an ordered mapping entry.
	PARSE_FLOW_MAPPING_FIRST_KEY_STATE            // Expect the first key of a flow mapping.
	PARSE_FLOW_MAPPING_KEY_STATE                  // Expect a key of a flow mapping.
	PARSE_FLOW_MAPPING_VALUE_STATE                // Expect a value of a flow mapping.
	PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE          // Expect an empty value of a flow mapping.
	PARSE_END_STATE                               // Expect nothing.
)

func (ps ParserState) String() string {
	switch ps {
	case PARSE_STREAM_START_STATE:
		return "PARSE_STREAM_START_STATE"
	case PARSE_IMPLICIT_DOCUMENT_START_STATE:
		return "PARSE_IMPLICIT_DOCUMENT_START_STATE"
	case PARSE_DOCUMENT_START_STATE:
		return "PARSE_DOCUMENT_START_STATE"
	case PARSE_DOCUMENT_CONTENT_STATE:
		return "PARSE_DOCUMENT_CONTENT_STATE"
	case PARSE_DOCUMENT_END_STATE:
		return "PARSE_DOCUMENT_END_STATE"
	case PARSE_BLOCK_NODE_STATE:
		return "PARSE_BLOCK_NODE_STATE"
	case PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE:
		return "PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE"
	case PARSE_BLOCK_SEQUENCE_ENTRY_STATE:
		return "PARSE_BLOCK_SEQUENCE_ENTRY_STATE"
	case PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE:
		return "PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE"
	case PARSE_BLOCK_MAPPING_FIRST_KEY_STATE:
		return "PARSE_BLOCK_MAPPING_FIRST_KEY_STATE"
	case PARSE_BLOCK_MAPPING_KEY_STATE:
		return "PARSE_BLOCK_MAPPING_KEY_STATE"
	case PARSE_BLOCK_MAPPING_VALUE_STATE:
		return "PARSE_BLOCK_MAPPING_VALUE_STATE"
	case PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE:
		return "PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE"
	case PARSE_FLOW_SEQUENCE_ENTRY_STATE:
		return "PARSE_FLOW_SEQUENCE_ENTRY_STATE"
	case PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE:
		return "PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE"
	case PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE:
		return "PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE"
	case PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE:
		return "PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE"
	case PARSE_FLOW_MAPPING_FIRST_KEY_STATE:
		return "PARSE_FLOW_MAPPING_FIRST_KEY_STATE"
	case PARSE_FLOW_MAPPING_KEY_STATE:
		return "PARSE_FLOW_MAPPING_KEY_STATE"
	case PARSE_FLOW_MAPPING_VALUE_STATE:
		return "PARSE_FLOW_MAPPING_VALUE_STATE"
	case PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE:
		return "PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE"
	case PARSE_END_STATE:
		return "PARSE_END_STATE"
	}
	return "<unknown parser state>"
}

// AliasData holds information about aliases.
type AliasData struct {
	anchor []byte // The anchor.
	index  int    // The node id.
	mark   Mark   // The anchor mark.
}

// Parser structure holds all information about the current
// state of the parser.
type Parser struct {
	lastError error

	// Reader stuff
	read_handler ReadHandler // Read handler.

	input_reader io.Reader // File input data.
	input        []byte    // String input data.
	input_pos    int

	eof bool // EOF flag

	buffer     []byte // The working buffer.
	buffer_pos int    // The current position of the buffer.

	unread int // The number of unread characters in the buffer.

	newlines int // The number of line breaks since last non-break/non-blank character

	raw_buffer     []byte // The raw buffer.
	raw_buffer_pos int    // The current position of the buffer.

	encoding Encoding // The input encoding.

	offset int  // The offset of the current position (in bytes).
	mark   Mark // The mark of the current position.

	// Comments

	HeadComment  []byte // The current head comments
	LineComment  []byte // The current line comments
	FootComment  []byte // The current foot comments
	tail_comment []byte // Foot comment that happens at the end of a block.
	stem_comment []byte // Comment in item preceding a nested structure (list inside list item, etc)

	comments      []Comment // The folded comments for all parsed tokens
	comments_head int

	// Scanner stuff

	stream_start_produced bool // Have we started to scan the input stream?
	stream_end_produced   bool // Have we reached the end of the input stream?

	flow_level int // The number of unclosed '[' and '{' indicators.

	tokens          []Token // The tokens queue.
	tokens_head     int     // The head of the tokens queue.
	tokens_parsed   int     // The number of tokens fetched from the queue.
	token_available bool    // Does the tokens queue contain a token ready for dequeueing.

	indent  int   // The current indentation level.
	indents []int // The indentation levels stack.

	simple_key_allowed  bool        // May a simple key occur at the current position?
	simple_key_possible bool        // Is the current simple key possible?
	simple_key          SimpleKey   // The current simple key.
	simple_key_stack    []SimpleKey // The stack of simple keys.

	// Parser stuff

	state          ParserState    // The current parser state.
	states         []ParserState  // The parser states stack.
	marks          []Mark         // The stack of marks.
	tag_directives []TagDirective // The list of TAG directives.

	// Representer stuff

	aliases []AliasData // The alias data.

	document *Document // The currently parsed document.
}

type Comment struct {
	ScanMark  Mark // Position where scanning for comments started
	TokenMark Mark // Position after which tokens will be associated with this comment
	StartMark Mark // Position of '#' comment mark
	EndMark   Mark // Position where comment terminated

	Head []byte
	Line []byte
	Foot []byte
}

// Emitter Definitions

// WriteHandler is called when the [Emitter] needs to flush the accumulated
// characters to the output.  The handler should write @a size bytes of the
// @a buffer to the output.
//
// @param[in,out]   data        A pointer to an application data specified by
//
//	yamlEmitter.setOutput().
//
// @param[in]       buffer      The buffer with bytes to be written.
// @param[in]       size        The size of the buffer.
//
// @returns On success, the handler should return @c 1.  If the handler failed,
// the returned value should be @c 0.
type WriteHandler func(emitter *Emitter, buffer []byte) error

type EmitterState int

// The emitter states.
const (
	// Expect STREAM-START.
	EMIT_STREAM_START_STATE EmitterState = iota

	EMIT_FIRST_DOCUMENT_START_STATE       // Expect the first DOCUMENT-START or STREAM-END.
	EMIT_DOCUMENT_START_STATE             // Expect DOCUMENT-START or STREAM-END.
	EMIT_DOCUMENT_CONTENT_STATE           // Expect the content of a document.
	EMIT_DOCUMENT_END_STATE               // Expect DOCUMENT-END.
	EMIT_FLOW_SEQUENCE_FIRST_ITEM_STATE   // Expect the first item of a flow sequence.
	EMIT_FLOW_SEQUENCE_TRAIL_ITEM_STATE   // Expect the next item of a flow sequence, with the comma already written out
	EMIT_FLOW_SEQUENCE_ITEM_STATE         // Expect an item of a flow sequence.
	EMIT_FLOW_MAPPING_FIRST_KEY_STATE     // Expect the first key of a flow mapping.
	EMIT_FLOW_MAPPING_TRAIL_KEY_STATE     // Expect the next key of a flow mapping, with the comma already written out
	EMIT_FLOW_MAPPING_KEY_STATE           // Expect a key of a flow mapping.
	EMIT_FLOW_MAPPING_SIMPLE_VALUE_STATE  // Expect a value for a simple key of a flow mapping.
	EMIT_FLOW_MAPPING_VALUE_STATE         // Expect a value of a flow mapping.
	EMIT_BLOCK_SEQUENCE_FIRST_ITEM_STATE  // Expect the first item of a block sequence.
	EMIT_BLOCK_SEQUENCE_ITEM_STATE        // Expect an item of a block sequence.
	EMIT_BLOCK_MAPPING_FIRST_KEY_STATE    // Expect the first key of a block mapping.
	EMIT_BLOCK_MAPPING_KEY_STATE          // Expect the key of a block mapping.
	EMIT_BLOCK_MAPPING_SIMPLE_VALUE_STATE // Expect a value for a simple key of a block mapping.
	EMIT_BLOCK_MAPPING_VALUE_STATE        // Expect a value of a block mapping.
	EMIT_END_STATE                        // Expect nothing.
)

// Emitter holds all information about the current state of the emitter.
type Emitter struct {
	// Writer stuff

	write_handler WriteHandler // Write handler.

	output_buffer *[]byte   // String output data.
	output_writer io.Writer // File output data.

	buffer     []byte // The working buffer.
	buffer_pos int    // The current position of the buffer.

	encoding Encoding // The stream encoding.

	// Emitter stuff

	canonical       bool       // If the output is in the canonical style?
	BestIndent      int        // The number of indentation spaces.
	best_width      int        // The preferred width of the output lines.
	unicode         bool       // Allow unescaped non-ASCII characters?
	line_break      LineBreak  // The preferred line break.
	quotePreference QuoteStyle // Preferred quote style when quoting is required.

	state  EmitterState   // The current emitter state.
	states []EmitterState // The stack of states.

	events      []Event // The event queue.
	events_head int     // The head of the event queue.

	indents []int // The stack of indentation levels.

	tag_directives []TagDirective // The list of tag directives.

	indent int // The current indentation level.

	CompactSequenceIndent bool // Is '- ' is considered part of the indentation for sequence elements?

	flow_level int // The current flow level.

	root_context       bool // Is it the document root context?
	sequence_context   bool // Is it a sequence context?
	mapping_context    bool // Is it a mapping context?
	simple_key_context bool // Is it a simple mapping key context?

	line       int  // The current line.
	column     int  // The current column.
	whitespace bool // If the last character was a whitespace?
	indention  bool // If the last character was an indentation character (' ', '-', '?', ':')?
	OpenEnded  bool // If an explicit document end is required?

	space_above bool // Is there's an empty line above?
	foot_indent int  // The indent used to write the foot comment above, or -1 if none.

	// Anchor analysis.
	anchor_data struct {
		anchor []byte // The anchor value.
		alias  bool   // Is it an alias?
	}

	// Tag analysis.
	tag_data struct {
		handle []byte // The tag handle.
		suffix []byte // The tag suffix.
	}

	// Scalar analysis.
	scalar_data struct {
		value                 []byte      // The scalar value.
		multiline             bool        // Does the scalar contain line breaks?
		flow_plain_allowed    bool        // Can the scalar be expressed in the flow plain style?
		block_plain_allowed   bool        // Can the scalar be expressed in the block plain style?
		single_quoted_allowed bool        // Can the scalar be expressed in the single quoted style?
		block_allowed         bool        // Can the scalar be expressed in the literal or folded styles?
		style                 ScalarStyle // The output style.
	}

	// Comments
	HeadComment []byte
	LineComment []byte
	FootComment []byte
	TailComment []byte

	key_line_comment []byte

	// Representer stuff

	opened bool // If the stream was already opened?
	closed bool // If the stream was already closed?

	// The information associated with the document nodes.
	anchors *struct {
		references int  // The number of references.
		anchor     int  // The anchor id.
		serialized bool // If the node has been emitted?
	}

	last_anchor_id int // The last assigned anchor id.

	document *Document // The currently emitted document.
}
