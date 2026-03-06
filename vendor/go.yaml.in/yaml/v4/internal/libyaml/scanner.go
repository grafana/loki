// Copyright 2006-2010 Kirill Simonov
// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0 AND MIT

// Scanner stage: Transforms input stream into token sequence.
// The Scanner is the most complex stage, handling indentation, simple keys,
// and block collection detection.

package libyaml

import (
	"bytes"
	"fmt"
	"io"
)

// Introduction
// ************
//
// The following notes assume that you are familiar with the YAML specification
// (http://yaml.org/spec/1.2/spec.html).  We mostly follow it, although in
// some cases we are less restrictive that it requires.
//
// The process of transforming a YAML stream into a sequence of events is
// divided on two steps: Scanning and Parsing.
//
// The Scanner transforms the input stream into a sequence of tokens, while the
// parser transform the sequence of tokens produced by the Scanner into a
// sequence of parsing events.
//
// The Scanner is rather clever and complicated. The Parser, on the contrary,
// is a straightforward implementation of a recursive-descendant parser (or,
// LL(1) parser, as it is usually called).
//
// Actually there are two issues of Scanning that might be called "clever", the
// rest is quite straightforward.  The issues are "block collection start" and
// "simple keys".  Both issues are explained below in details.
//
// Here the Scanning step is explained and implemented.  We start with the list
// of all the tokens produced by the Scanner together with short descriptions.
//
// Now, tokens:
//
//      STREAM-START(encoding)          # The stream start.
//      STREAM-END                      # The stream end.
//      VERSION-DIRECTIVE(major,minor)  # The '%YAML' directive.
//      TAG-DIRECTIVE(handle,prefix)    # The '%TAG' directive.
//      DOCUMENT-START                  # '---'
//      DOCUMENT-END                    # '...'
//      BLOCK-SEQUENCE-START            # Indentation increase denoting a block
//      BLOCK-MAPPING-START             # sequence or a block mapping.
//      BLOCK-END                       # Indentation decrease.
//      FLOW-SEQUENCE-START             # '['
//      FLOW-SEQUENCE-END               # ']'
//      BLOCK-SEQUENCE-START            # '{'
//      BLOCK-SEQUENCE-END              # '}'
//      BLOCK-ENTRY                     # '-'
//      FLOW-ENTRY                      # ','
//      KEY                             # '?' or nothing (simple keys).
//      VALUE                           # ':'
//      ALIAS(anchor)                   # '*anchor'
//      ANCHOR(anchor)                  # '&anchor'
//      TAG(handle,suffix)              # '!handle!suffix'
//      SCALAR(value,style)             # A scalar.
//
// The following two tokens are "virtual" tokens denoting the beginning and the
// end of the stream:
//
//      STREAM-START(encoding)
//      STREAM-END
//
// We pass the information about the input stream encoding with the
// STREAM-START token.
//
// The next two tokens are responsible for tags:
//
//      VERSION-DIRECTIVE(major,minor)
//      TAG-DIRECTIVE(handle,prefix)
//
// Example:
//
//      %YAML   1.1
//      %TAG    !   !foo
//      %TAG    !yaml!  tag:yaml.org,2002:
//      ---
//
// The corresponding sequence of tokens:
//
//      STREAM-START(utf-8)
//      VERSION-DIRECTIVE(1,1)
//      TAG-DIRECTIVE("!","!foo")
//      TAG-DIRECTIVE("!yaml","tag:yaml.org,2002:")
//      DOCUMENT-START
//      STREAM-END
//
// Note that the VERSION-DIRECTIVE and TAG-DIRECTIVE tokens occupy a whole
// line.
//
// The document start and end indicators are represented by:
//
//      DOCUMENT-START
//      DOCUMENT-END
//
// Note that if a YAML stream contains an implicit document (without '---'
// and '...' indicators), no DOCUMENT-START and DOCUMENT-END tokens will be
// produced.
//
// In the following examples, we present whole documents together with the
// produced tokens.
//
//      1. An implicit document:
//
//          'a scalar'
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          SCALAR("a scalar",single-quoted)
//          STREAM-END
//
//      2. An explicit document:
//
//          ---
//          'a scalar'
//          ...
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          DOCUMENT-START
//          SCALAR("a scalar",single-quoted)
//          DOCUMENT-END
//          STREAM-END
//
//      3. Several documents in a stream:
//
//          'a scalar'
//          ---
//          'another scalar'
//          ---
//          'yet another scalar'
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          SCALAR("a scalar",single-quoted)
//          DOCUMENT-START
//          SCALAR("another scalar",single-quoted)
//          DOCUMENT-START
//          SCALAR("yet another scalar",single-quoted)
//          STREAM-END
//
// We have already introduced the SCALAR token above.  The following tokens are
// used to describe aliases, anchors, tag, and scalars:
//
//      ALIAS(anchor)
//      ANCHOR(anchor)
//      TAG(handle,suffix)
//      SCALAR(value,style)
//
// The following series of examples illustrate the usage of these tokens:
//
//      1. A recursive sequence:
//
//          &A [ *A ]
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          ANCHOR("A")
//          FLOW-SEQUENCE-START
//          ALIAS("A")
//          FLOW-SEQUENCE-END
//          STREAM-END
//
//      2. A tagged scalar:
//
//          !!float "3.14"  # A good approximation.
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          TAG("!!","float")
//          SCALAR("3.14",double-quoted)
//          STREAM-END
//
//      3. Various scalar styles:
//
//          --- # Implicit empty plain scalars do not produce tokens.
//          --- a plain scalar
//          --- 'a single-quoted scalar'
//          --- "a double-quoted scalar"
//          --- |-
//            a literal scalar
//          --- >-
//            a folded
//            scalar
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          DOCUMENT-START
//          DOCUMENT-START
//          SCALAR("a plain scalar",plain)
//          DOCUMENT-START
//          SCALAR("a single-quoted scalar",single-quoted)
//          DOCUMENT-START
//          SCALAR("a double-quoted scalar",double-quoted)
//          DOCUMENT-START
//          SCALAR("a literal scalar",literal)
//          DOCUMENT-START
//          SCALAR("a folded scalar",folded)
//          STREAM-END
//
// Now it's time to review collection-related tokens. We will start with
// flow collections:
//
//      FLOW-SEQUENCE-START
//      FLOW-SEQUENCE-END
//      FLOW-MAPPING-START
//      FLOW-MAPPING-END
//      FLOW-ENTRY
//      KEY
//      VALUE
//
// The tokens FLOW-SEQUENCE-START, FLOW-SEQUENCE-END, FLOW-MAPPING-START, and
// FLOW-MAPPING-END represent the indicators '[', ']', '{', and '}'
// correspondingly.  FLOW-ENTRY represent the ',' indicator.  Finally the
// indicators '?' and ':', which are used for denoting mapping keys and values,
// are represented by the KEY and VALUE tokens.
//
// The following examples show flow collections:
//
//      1. A flow sequence:
//
//          [item 1, item 2, item 3]
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          FLOW-SEQUENCE-START
//          SCALAR("item 1",plain)
//          FLOW-ENTRY
//          SCALAR("item 2",plain)
//          FLOW-ENTRY
//          SCALAR("item 3",plain)
//          FLOW-SEQUENCE-END
//          STREAM-END
//
//      2. A flow mapping:
//
//          {
//              a simple key: a value,  # Note that the KEY token is produced.
//              ? a complex key: another value,
//          }
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          FLOW-MAPPING-START
//          KEY
//          SCALAR("a simple key",plain)
//          VALUE
//          SCALAR("a value",plain)
//          FLOW-ENTRY
//          KEY
//          SCALAR("a complex key",plain)
//          VALUE
//          SCALAR("another value",plain)
//          FLOW-ENTRY
//          FLOW-MAPPING-END
//          STREAM-END
//
// A simple key is a key which is not denoted by the '?' indicator.  Note that
// the Scanner still produce the KEY token whenever it encounters a simple key.
//
// For scanning block collections, the following tokens are used (note that we
// repeat KEY and VALUE here):
//
//      BLOCK-SEQUENCE-START
//      BLOCK-MAPPING-START
//      BLOCK-END
//      BLOCK-ENTRY
//      KEY
//      VALUE
//
// The tokens BLOCK-SEQUENCE-START and BLOCK-MAPPING-START denote indentation
// increase that precedes a block collection (cf. the INDENT token in Python).
// The token BLOCK-END denote indentation decrease that ends a block collection
// (cf. the DEDENT token in Python).  However YAML has some syntax peculiarities
// that makes detections of these tokens more complex.
//
// The tokens BLOCK-ENTRY, KEY, and VALUE are used to represent the indicators
// '-', '?', and ':' correspondingly.
//
// The following examples show how the tokens BLOCK-SEQUENCE-START,
// BLOCK-MAPPING-START, and BLOCK-END are emitted by the Scanner:
//
//      1. Block sequences:
//
//          - item 1
//          - item 2
//          -
//            - item 3.1
//            - item 3.2
//          -
//            key 1: value 1
//            key 2: value 2
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          SCALAR("item 1",plain)
//          BLOCK-ENTRY
//          SCALAR("item 2",plain)
//          BLOCK-ENTRY
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          SCALAR("item 3.1",plain)
//          BLOCK-ENTRY
//          SCALAR("item 3.2",plain)
//          BLOCK-END
//          BLOCK-ENTRY
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("key 1",plain)
//          VALUE
//          SCALAR("value 1",plain)
//          KEY
//          SCALAR("key 2",plain)
//          VALUE
//          SCALAR("value 2",plain)
//          BLOCK-END
//          BLOCK-END
//          STREAM-END
//
//      2. Block mappings:
//
//          a simple key: a value   # The KEY token is produced here.
//          ? a complex key
//          : another value
//          a mapping:
//            key 1: value 1
//            key 2: value 2
//          a sequence:
//            - item 1
//            - item 2
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("a simple key",plain)
//          VALUE
//          SCALAR("a value",plain)
//          KEY
//          SCALAR("a complex key",plain)
//          VALUE
//          SCALAR("another value",plain)
//          KEY
//          SCALAR("a mapping",plain)
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("key 1",plain)
//          VALUE
//          SCALAR("value 1",plain)
//          KEY
//          SCALAR("key 2",plain)
//          VALUE
//          SCALAR("value 2",plain)
//          BLOCK-END
//          KEY
//          SCALAR("a sequence",plain)
//          VALUE
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          SCALAR("item 1",plain)
//          BLOCK-ENTRY
//          SCALAR("item 2",plain)
//          BLOCK-END
//          BLOCK-END
//          STREAM-END
//
// YAML does not always require to start a new block collection from a new
// line.  If the current line contains only '-', '?', and ':' indicators, a new
// block collection may start at the current line.  The following examples
// illustrate this case:
//
//      1. Collections in a sequence:
//
//          - - item 1
//            - item 2
//          - key 1: value 1
//            key 2: value 2
//          - ? complex key
//            : complex value
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          SCALAR("item 1",plain)
//          BLOCK-ENTRY
//          SCALAR("item 2",plain)
//          BLOCK-END
//          BLOCK-ENTRY
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("key 1",plain)
//          VALUE
//          SCALAR("value 1",plain)
//          KEY
//          SCALAR("key 2",plain)
//          VALUE
//          SCALAR("value 2",plain)
//          BLOCK-END
//          BLOCK-ENTRY
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("complex key")
//          VALUE
//          SCALAR("complex value")
//          BLOCK-END
//          BLOCK-END
//          STREAM-END
//
//      2. Collections in a mapping:
//
//          ? a sequence
//          : - item 1
//            - item 2
//          ? a mapping
//          : key 1: value 1
//            key 2: value 2
//
//      Tokens:
//
//          STREAM-START(utf-8)
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("a sequence",plain)
//          VALUE
//          BLOCK-SEQUENCE-START
//          BLOCK-ENTRY
//          SCALAR("item 1",plain)
//          BLOCK-ENTRY
//          SCALAR("item 2",plain)
//          BLOCK-END
//          KEY
//          SCALAR("a mapping",plain)
//          VALUE
//          BLOCK-MAPPING-START
//          KEY
//          SCALAR("key 1",plain)
//          VALUE
//          SCALAR("value 1",plain)
//          KEY
//          SCALAR("key 2",plain)
//          VALUE
//          SCALAR("value 2",plain)
//          BLOCK-END
//          BLOCK-END
//          STREAM-END
//
// YAML also permits non-indented sequences if they are included into a block
// mapping.  In this case, the token BLOCK-SEQUENCE-START is not produced:
//
//      key:
//      - item 1    # BLOCK-SEQUENCE-START is NOT produced here.
//      - item 2
//
// Tokens:
//
//      STREAM-START(utf-8)
//      BLOCK-MAPPING-START
//      KEY
//      SCALAR("key",plain)
//      VALUE
//      BLOCK-ENTRY
//      SCALAR("item 1",plain)
//      BLOCK-ENTRY
//      SCALAR("item 2",plain)
//      BLOCK-END
//

// Advance the buffer pointer.
func (parser *Parser) skip() {
	if !isBlank(parser.buffer, parser.buffer_pos) {
		parser.newlines = 0
	}
	parser.mark.Index++
	parser.mark.Column++
	parser.unread--
	parser.buffer_pos += width(parser.buffer[parser.buffer_pos])
}

func (parser *Parser) skipLine() {
	if isCRLF(parser.buffer, parser.buffer_pos) {
		parser.mark.Index += 2
		parser.mark.Column = 0
		parser.mark.Line++
		parser.unread -= 2
		parser.buffer_pos += 2
		parser.newlines++
	} else if isLineBreak(parser.buffer, parser.buffer_pos) {
		parser.mark.Index++
		parser.mark.Column = 0
		parser.mark.Line++
		parser.unread--
		parser.buffer_pos += width(parser.buffer[parser.buffer_pos])
		parser.newlines++
	}
}

// Copy a character to a string buffer and advance pointers.
func (parser *Parser) read(s []byte) []byte {
	if !isBlank(parser.buffer, parser.buffer_pos) {
		parser.newlines = 0
	}
	w := width(parser.buffer[parser.buffer_pos])
	if w == 0 {
		panic("invalid character sequence")
	}
	if len(s) == 0 {
		s = make([]byte, 0, 32)
	}
	if w == 1 && len(s)+w <= cap(s) {
		s = s[:len(s)+1]
		s[len(s)-1] = parser.buffer[parser.buffer_pos]
		parser.buffer_pos++
	} else {
		s = append(s, parser.buffer[parser.buffer_pos:parser.buffer_pos+w]...)
		parser.buffer_pos += w
	}
	parser.mark.Index++
	parser.mark.Column++
	parser.unread--
	return s
}

// Copy a line break character to a string buffer and advance pointers.
func (parser *Parser) readLine(s []byte) []byte {
	buf := parser.buffer
	pos := parser.buffer_pos
	switch {
	case buf[pos] == '\r' && buf[pos+1] == '\n':
		// CR LF . LF
		s = append(s, '\n')
		parser.buffer_pos += 2
		parser.mark.Index++
		parser.unread--
	case buf[pos] == '\r' || buf[pos] == '\n':
		// CR|LF . LF
		s = append(s, '\n')
		parser.buffer_pos += 1
	case buf[pos] == '\xC2' && buf[pos+1] == '\x85':
		// NEL . LF
		s = append(s, '\n')
		parser.buffer_pos += 2
	case buf[pos] == '\xE2' && buf[pos+1] == '\x80' && (buf[pos+2] == '\xA8' || buf[pos+2] == '\xA9'):
		// LS|PS . LS|PS
		s = append(s, buf[parser.buffer_pos:pos+3]...)
		parser.buffer_pos += 3
	default:
		return s
	}
	parser.mark.Index++
	parser.mark.Column = 0
	parser.mark.Line++
	parser.unread--
	parser.newlines++
	return s
}

// Scan gets the next token.
func (parser *Parser) Scan(token *Token) error {
	// Erase the token object.
	*token = Token{} // [Go] Is this necessary?

	if parser.lastError != nil {
		return parser.lastError
	}

	// No tokens after STREAM-END or error.
	if parser.stream_end_produced {
		return io.EOF
	}

	// Ensure that the tokens queue contains enough tokens.
	if !parser.token_available {
		if err := parser.fetchMoreTokens(); err != nil {
			parser.lastError = err
			return err
		}
	}

	// Fetch the next token from the queue.
	*token = parser.tokens[parser.tokens_head]
	parser.tokens_head++
	parser.tokens_parsed++
	parser.token_available = false

	if token.Type == STREAM_END_TOKEN {
		parser.stream_end_produced = true
	}
	return nil
}

func formatScannerError(problem string, problem_mark Mark) error {
	problem_mark.Line += 1

	return ScannerError{
		Mark:    problem_mark,
		Message: problem,
	}
}

func formatScannerErrorContext(context string, context_mark Mark, problem string, problem_mark Mark) error {
	context_mark.Line += 1
	problem_mark.Line += 1

	return ScannerError{
		ContextMark:    context_mark,
		ContextMessage: context,

		Mark:    problem_mark,
		Message: problem,
	}
}

func (parser *Parser) setScannerTagError(directive bool, context_mark Mark, problem string) error {
	context := "while parsing a tag"
	if directive {
		context = "while parsing a %TAG directive"
	}
	return formatScannerErrorContext(context, context_mark, problem, parser.mark)
}

func trace(args ...any) func() {
	pargs := append([]any{"+++"}, args...)
	fmt.Println(pargs...)
	pargs = append([]any{"---"}, args...)
	return func() { fmt.Println(pargs...) }
}

// Ensure that the tokens queue contains at least one token which can be
// returned to the Parser.
func (parser *Parser) fetchMoreTokens() error {
	// While we need more tokens to fetch, do it.
	for {
		// [Go] The comment parsing logic requires a lookahead of two tokens
		// so that foot comments may be parsed in time of associating them
		// with the tokens that are parsed before them, and also for line
		// comments to be transformed into head comments in some edge cases.
		if parser.tokens_head < len(parser.tokens)-2 {
			// If a potential simple key is at the head position, we need to fetch
			// the next token to disambiguate it.

			var first_key int
			found_potential_key := false

			if len(parser.simple_key_stack) > 0 {
				// Found a simple key on the stack
				first_key = parser.simple_key_stack[0].token_number
				found_potential_key = true
			} else if parser.simple_key_possible {
				// Found a 'current' simple key (which was not pushed to the stack yet)
				first_key = parser.simple_key.token_number
				found_potential_key = true
			}

			if !found_potential_key {
				// We don't have any potential simple keys
				break
			} else if parser.tokens_parsed != first_key {
				// We have not reached the potential simple key yet.
				break
			}
		}
		// Fetch the next token.
		if err := parser.fetchNextToken(); err != nil {
			return err
		}
	}

	parser.token_available = true
	return nil
}

// The dispatcher for token fetchers.
func (parser *Parser) fetchNextToken() (err error) {
	// Ensure that the buffer is initialized.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	// Check if we just started scanning.  Fetch STREAM-START then.
	if !parser.stream_start_produced {
		return parser.fetchStreamStart()
	}

	scan_mark := parser.mark

	// Eat whitespaces and comments until we reach the next token.
	if err := parser.scanToNextToken(); err != nil {
		return err
	}

	// [Go] While unrolling indents, transform the head comments of prior
	// indentation levels observed after scan_start into foot comments at
	// the respective indexes.

	// Check the indentation level against the current column.
	if err := parser.unrollIndent(parser.mark.Column, scan_mark); err != nil {
		return err
	}

	// Ensure that the buffer contains at least 4 characters.  4 is the length
	// of the longest indicators ('--- ' and '... ').
	if parser.unread < 4 {
		if err := parser.updateBuffer(4); err != nil {
			return err
		}
	}

	// Is it the end of the stream?
	if isZeroChar(parser.buffer, parser.buffer_pos) {
		return parser.fetchStreamEnd()
	}

	// Is it a directive?
	if parser.mark.Column == 0 && parser.buffer[parser.buffer_pos] == '%' {
		return parser.fetchDirective()
	}

	buf := parser.buffer
	pos := parser.buffer_pos

	// Is it the document start indicator?
	if parser.mark.Column == 0 && buf[pos] == '-' && buf[pos+1] == '-' && buf[pos+2] == '-' && isBlankOrZero(buf, pos+3) {
		return parser.fetchDocumentIndicator(DOCUMENT_START_TOKEN)
	}

	// Is it the document end indicator?
	if parser.mark.Column == 0 && buf[pos] == '.' && buf[pos+1] == '.' && buf[pos+2] == '.' && isBlankOrZero(buf, pos+3) {
		return parser.fetchDocumentIndicator(DOCUMENT_END_TOKEN)
	}

	comment_mark := parser.mark
	if len(parser.tokens) > 0 && (parser.flow_level == 0 && buf[pos] == ':' || parser.flow_level > 0 && buf[pos] == ',') {
		// Associate any following comments with the prior token.
		comment_mark = parser.tokens[len(parser.tokens)-1].StartMark
	}
	defer func() {
		if err != nil {
			return
		}
		if len(parser.tokens) > 0 && parser.tokens[len(parser.tokens)-1].Type == BLOCK_ENTRY_TOKEN {
			// Sequence indicators alone have no line comments. It becomes
			// a head comment for whatever follows.
			return
		}
		err = parser.scanLineComment(comment_mark)
	}()

	// Is it the flow sequence start indicator?
	if buf[pos] == '[' {
		return parser.fetchFlowCollectionStart(FLOW_SEQUENCE_START_TOKEN)
	}

	// Is it the flow mapping start indicator?
	if parser.buffer[parser.buffer_pos] == '{' {
		return parser.fetchFlowCollectionStart(FLOW_MAPPING_START_TOKEN)
	}

	// Is it the flow sequence end indicator?
	if parser.buffer[parser.buffer_pos] == ']' {
		return parser.fetchFlowCollectionEnd(
			FLOW_SEQUENCE_END_TOKEN)
	}

	// Is it the flow mapping end indicator?
	if parser.buffer[parser.buffer_pos] == '}' {
		return parser.fetchFlowCollectionEnd(
			FLOW_MAPPING_END_TOKEN)
	}

	// Is it the flow entry indicator?
	if parser.buffer[parser.buffer_pos] == ',' {
		return parser.fetchFlowEntry()
	}

	// Is it the block entry indicator?
	if parser.buffer[parser.buffer_pos] == '-' && isBlankOrZero(parser.buffer, parser.buffer_pos+1) {
		return parser.fetchBlockEntry()
	}

	// Is it the key indicator?
	if parser.buffer[parser.buffer_pos] == '?' && isBlankOrZero(parser.buffer, parser.buffer_pos+1) {
		return parser.fetchKey()
	}

	// Is it the value indicator?
	if parser.buffer[parser.buffer_pos] == ':' && (parser.flow_level > 0 && !parser.isFlowSequence() || isBlankOrZero(parser.buffer, parser.buffer_pos+1)) {
		return parser.fetchValue()
	}

	// Is it an alias?
	if parser.buffer[parser.buffer_pos] == '*' {
		return parser.fetchAnchor(ALIAS_TOKEN)
	}

	// Is it an anchor?
	if parser.buffer[parser.buffer_pos] == '&' {
		return parser.fetchAnchor(ANCHOR_TOKEN)
	}

	// Is it a tag?
	if parser.buffer[parser.buffer_pos] == '!' {
		return parser.fetchTag()
	}

	// Is it a literal scalar?
	if parser.buffer[parser.buffer_pos] == '|' && parser.flow_level == 0 {
		return parser.fetchBlockScalar(true)
	}

	// Is it a folded scalar?
	if parser.buffer[parser.buffer_pos] == '>' && parser.flow_level == 0 {
		return parser.fetchBlockScalar(false)
	}

	// Is it a single-quoted scalar?
	if parser.buffer[parser.buffer_pos] == '\'' {
		return parser.fetchFlowScalar(true)
	}

	// Is it a double-quoted scalar?
	if parser.buffer[parser.buffer_pos] == '"' {
		return parser.fetchFlowScalar(false)
	}

	// Is it a plain scalar?
	//
	// A plain scalar may start with any non-blank characters except
	//
	//      '-', '?', ':', ',', '[', ']', '{', '}',
	//      '#', '&', '*', '!', '|', '>', '\'', '\"',
	//      '%', '@', '`'.
	//
	// In the block context (and, for the '-' indicator, in the flow context
	// too), it may also start with the characters
	//
	//      '-', '?', ':'
	//
	// if it is followed by a non-space character.
	//
	// The last rule is more restrictive than the specification requires.
	// [Go] TODO Make this logic more reasonable.
	//switch parser.buffer[parser.buffer_pos] {
	//case '-', '?', ':', ',', '?', '-', ',', ':', ']', '[', '}', '{', '&', '#', '!', '*', '>', '|', '"', '\'', '@', '%', '-', '`':
	//}
	if !(isBlankOrZero(parser.buffer, parser.buffer_pos) || parser.buffer[parser.buffer_pos] == '-' ||
		parser.buffer[parser.buffer_pos] == '?' || parser.buffer[parser.buffer_pos] == ':' ||
		parser.buffer[parser.buffer_pos] == ',' || parser.buffer[parser.buffer_pos] == '[' ||
		parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '{' ||
		parser.buffer[parser.buffer_pos] == '}' || parser.buffer[parser.buffer_pos] == '#' ||
		parser.buffer[parser.buffer_pos] == '&' || parser.buffer[parser.buffer_pos] == '*' ||
		parser.buffer[parser.buffer_pos] == '!' || parser.buffer[parser.buffer_pos] == '|' ||
		parser.buffer[parser.buffer_pos] == '>' || parser.buffer[parser.buffer_pos] == '\'' ||
		parser.buffer[parser.buffer_pos] == '"' || parser.buffer[parser.buffer_pos] == '%' ||
		parser.buffer[parser.buffer_pos] == '@' || parser.buffer[parser.buffer_pos] == '`') ||
		(parser.buffer[parser.buffer_pos] == '-' && !isBlank(parser.buffer, parser.buffer_pos+1)) ||
		((parser.buffer[parser.buffer_pos] == '?' || parser.buffer[parser.buffer_pos] == ':') &&
			!isBlankOrZero(parser.buffer, parser.buffer_pos+1)) {
		return parser.fetchPlainScalar()
	}

	// If we don't determine the token type so far, it is an error.
	return formatScannerErrorContext(
		"while scanning for the next token", parser.mark,
		"found character that cannot start any token", parser.mark)
}

func (parser *Parser) isFlowSequence() bool {
	if len(parser.tokens) == 0 {
		return false
	}
	previousToken := parser.tokens[len(parser.tokens)-1]
	return previousToken.Type == FLOW_ENTRY_TOKEN || previousToken.Type == FLOW_SEQUENCE_START_TOKEN
}

// Check if a simple key may start at the current position and add it if
// needed.
func (parser *Parser) saveSimpleKey() error {
	// A simple key is required at the current position if the scanner is in
	// the block context and the current column coincides with the indentation
	// level.

	required := parser.flow_level == 0 && parser.indent == parser.mark.Column

	//
	// If the current position may start a simple key, save it.
	//
	if parser.simple_key_allowed {
		if err := parser.removeSimpleKey(); err != nil {
			return err
		}

		parser.simple_key_possible = true
		parser.simple_key = SimpleKey{
			required:     required,
			flow_level:   parser.flow_level,
			token_number: parser.tokens_parsed + (len(parser.tokens) - parser.tokens_head),
			mark:         parser.mark,
		}
	}
	return nil
}

// Remove a potential simple key at the current flow level.
func (parser *Parser) removeSimpleKey() error {
	// If the key is required, it is an error.
	if parser.simple_key.required {
		return formatScannerErrorContext(
			"while scanning a simple key", parser.simple_key.mark,
			"could not find expected ':'", parser.mark)
	}

	parser.simple_key_possible = false // disable the key
	return nil
}

// max_flow_level limits the flow_level
const max_flow_level = 10000

// Increase the flow level and resize the simple key list if needed.
func (parser *Parser) increaseFlowLevel() error {
	// Increase the flow level.
	parser.flow_level++
	if parser.flow_level > max_flow_level {
		return formatScannerErrorContext(
			"while increasing flow level", parser.simple_key.mark,
			fmt.Sprintf("exceeded max depth of %d", max_flow_level), parser.mark)
	}

	// If a simple key was possible, push it to the stack before resetting the key.
	if parser.simple_key_possible {
		parser.simple_key_stack = append(parser.simple_key_stack, parser.simple_key)
	}

	// Reset the simple key for the new flow level.
	parser.simple_key = SimpleKey{}

	return nil
}

// Decrease the flow level.
func (parser *Parser) decreaseFlowLevel() error {
	if parser.flow_level > 0 {
		parser.flow_level--

		if len(parser.simple_key_stack) == 0 {
			return nil
		}

		last := len(parser.simple_key_stack) - 1
		if parser.simple_key_stack[last].flow_level == parser.flow_level {
			parser.simple_key = parser.simple_key_stack[last]        // use last item
			parser.simple_key_stack = parser.simple_key_stack[:last] // remove last item
			parser.simple_key_possible = true                        // enable the key
		}
	}
	return nil
}

// max_indents limits the indents stack size
const max_indents = 10000

// Push the current indentation level to the stack and set the new level
// the current column is greater than the indentation level.  In this case,
// append or insert the specified token into the token queue.
func (parser *Parser) rollIndent(column, number int, typ TokenType, mark Mark) error {
	// In the flow context, do nothing.
	if parser.flow_level > 0 {
		return nil
	}

	if parser.indent < column {
		// Push the current indentation level to the stack and set the new
		// indentation level.
		parser.indents = append(parser.indents, parser.indent)
		parser.indent = column
		if len(parser.indents) > max_indents {
			return formatScannerErrorContext(
				"while increasing indent level", parser.simple_key.mark,
				fmt.Sprintf("exceeded max depth of %d", max_indents), parser.mark)
		}

		// Create a token and insert it into the queue.
		token := Token{
			Type:      typ,
			StartMark: mark,
			EndMark:   mark,
		}
		if number > -1 {
			number -= parser.tokens_parsed
		}
		parser.insertToken(number, &token)
	}
	return nil
}

// Pop indentation levels from the indents stack until the current level
// becomes less or equal to the column.  For each indentation level, append
// the BLOCK-END token.
func (parser *Parser) unrollIndent(column int, scan_mark Mark) error {
	// In the flow context, do nothing.
	if parser.flow_level > 0 {
		return nil
	}

	block_mark := scan_mark
	block_mark.Index--

	// Loop through the indentation levels in the stack.
	for parser.indent > column {

		// [Go] Reposition the end token before potential following
		//      foot comments of parent blocks. For that, search
		//      backwards for recent comments that were at the same
		//      indent as the block that is ending now.
		stop_index := block_mark.Index
		for i := len(parser.comments) - 1; i >= 0; i-- {
			comment := &parser.comments[i]

			if comment.EndMark.Index < stop_index {
				// Don't go back beyond the start of the comment/whitespace scan, unless column < 0.
				// If requested indent column is < 0, then the document is over and everything else
				// is a foot anyway.
				break
			}
			if comment.StartMark.Column == parser.indent+1 {
				// This is a good match. But maybe there's a former comment
				// at that same indent level, so keep searching.
				block_mark = comment.StartMark
			}

			// While the end of the former comment matches with
			// the start of the following one, we know there's
			// nothing in between and scanning is still safe.
			stop_index = comment.ScanMark.Index
		}

		// Create a token and append it to the queue.
		token := Token{
			Type:      BLOCK_END_TOKEN,
			StartMark: block_mark,
			EndMark:   block_mark,
		}
		parser.insertToken(-1, &token)

		// Pop the indentation level.
		parser.indent = parser.indents[len(parser.indents)-1]
		parser.indents = parser.indents[:len(parser.indents)-1]
	}
	return nil
}

// Initialize the scanner and produce the STREAM-START token.
func (parser *Parser) fetchStreamStart() error {
	// Set the initial indentation.
	parser.indent = -1

	// Initialize the simple key stack.
	parser.simple_key = SimpleKey{}
	parser.simple_key_stack = []SimpleKey{}

	// A simple key is allowed at the beginning of the stream.
	parser.simple_key_allowed = true

	// We have started.
	parser.stream_start_produced = true

	// Create the STREAM-START token and append it to the queue.
	token := Token{
		Type:      STREAM_START_TOKEN,
		StartMark: parser.mark,
		EndMark:   parser.mark,
		encoding:  parser.encoding,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the STREAM-END token and shut down the scanner.
func (parser *Parser) fetchStreamEnd() error {
	// Force new line.
	if parser.mark.Column != 0 {
		parser.mark.Column = 0
		parser.mark.Line++
	}

	// Reset the indentation level.
	if err := parser.unrollIndent(-1, parser.mark); err != nil {
		return err
	}

	// Reset simple keys.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}
	parser.simple_key = SimpleKey{}
	parser.simple_key_stack = []SimpleKey{}
	parser.simple_key_allowed = false

	// Create the STREAM-END token and append it to the queue.
	token := Token{
		Type:      STREAM_END_TOKEN,
		StartMark: parser.mark,
		EndMark:   parser.mark,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce a VERSION-DIRECTIVE or TAG-DIRECTIVE token.
func (parser *Parser) fetchDirective() error {
	// Reset the indentation level.
	if err := parser.unrollIndent(-1, parser.mark); err != nil {
		return err
	}

	// Reset simple keys.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	parser.simple_key_allowed = false

	// Create the YAML-DIRECTIVE or TAG-DIRECTIVE token.
	token := Token{}
	if err := parser.scanDirective(&token); err != nil {
		return err
	}
	// Append the token to the queue.
	parser.insertToken(-1, &token)
	return nil
}

// Produce the DOCUMENT-START or DOCUMENT-END token.
func (parser *Parser) fetchDocumentIndicator(typ TokenType) error {
	// Reset the indentation level.
	if err := parser.unrollIndent(-1, parser.mark); err != nil {
		return err
	}

	// Reset simple keys.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	parser.simple_key_allowed = false

	// Consume the token.
	start_mark := parser.mark

	parser.skip()
	parser.skip()
	parser.skip()

	end_mark := parser.mark

	// Create the DOCUMENT-START or DOCUMENT-END token.
	token := Token{
		Type:      typ,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	// Append the token to the queue.
	parser.insertToken(-1, &token)
	return nil
}

// Produce the FLOW-SEQUENCE-START or FLOW-MAPPING-START token.
func (parser *Parser) fetchFlowCollectionStart(typ TokenType) error {
	// The indicators '[' and '{' may start a simple key.
	if err := parser.saveSimpleKey(); err != nil {
		return err
	}

	// Increase the flow level.
	if err := parser.increaseFlowLevel(); err != nil {
		return err
	}

	// A simple key may follow the indicators '[' and '{'.
	parser.simple_key_allowed = true

	// Consume the token.
	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the FLOW-SEQUENCE-START of FLOW-MAPPING-START token.
	token := Token{
		Type:      typ,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	// Append the token to the queue.
	parser.insertToken(-1, &token)
	return nil
}

// Produce the FLOW-SEQUENCE-END or FLOW-MAPPING-END token.
func (parser *Parser) fetchFlowCollectionEnd(typ TokenType) error {
	// Reset any potential simple key on the current flow level.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	// Decrease the flow level.
	if err := parser.decreaseFlowLevel(); err != nil {
		return err
	}

	// No simple keys after the indicators ']' and '}'.
	parser.simple_key_allowed = false

	// Consume the token.

	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the FLOW-SEQUENCE-END of FLOW-MAPPING-END token.
	token := Token{
		Type:      typ,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	// Append the token to the queue.
	parser.insertToken(-1, &token)
	return nil
}

// Produce the FLOW-ENTRY token.
func (parser *Parser) fetchFlowEntry() error {
	// Reset any potential simple keys on the current flow level.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	// Simple keys are allowed after ','.
	parser.simple_key_allowed = true

	// Consume the token.
	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the FLOW-ENTRY token and append it to the queue.
	token := Token{
		Type:      FLOW_ENTRY_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the BLOCK-ENTRY token.
func (parser *Parser) fetchBlockEntry() error {
	// Check if the scanner is in the block context.
	if parser.flow_level == 0 {
		// Check if we are allowed to start a new entry.
		if !parser.simple_key_allowed {
			return formatScannerError("block sequence entries are not allowed in this context", parser.mark)
		}
		// Add the BLOCK-SEQUENCE-START token if needed.
		if err := parser.rollIndent(parser.mark.Column, -1, BLOCK_SEQUENCE_START_TOKEN, parser.mark); err != nil {
			return err
		}
	} else { //nolint:staticcheck // there is no problem with this empty branch as it's documentation.

		// It is an error for the '-' indicator to occur in the flow context,
		// but we let the Parser detect and report about it because the Parser
		// is able to point to the context.
	}

	// Reset any potential simple keys on the current flow level.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	// Simple keys are allowed after '-'.
	parser.simple_key_allowed = true

	// Consume the token.
	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the BLOCK-ENTRY token and append it to the queue.
	token := Token{
		Type:      BLOCK_ENTRY_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the KEY token.
func (parser *Parser) fetchKey() error {
	// In the block context, additional checks are required.
	if parser.flow_level == 0 {
		// Check if we are allowed to start a new key (not necessary simple).
		if !parser.simple_key_allowed {
			return formatScannerError("mapping keys are not allowed in this context", parser.mark)
		}
		// Add the BLOCK-MAPPING-START token if needed.
		if err := parser.rollIndent(parser.mark.Column, -1, BLOCK_MAPPING_START_TOKEN, parser.mark); err != nil {
			return err
		}
	}

	// Reset any potential simple keys on the current flow level.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	// Simple keys are allowed after '?' in the block context.
	parser.simple_key_allowed = parser.flow_level == 0

	// Consume the token.
	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the KEY token and append it to the queue.
	token := Token{
		Type:      KEY_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the VALUE token.
func (parser *Parser) fetchValue() error {
	simple_key := &parser.simple_key

	// Have we found a simple key?
	if parser.simple_key_possible && simple_key.mark.Line == parser.mark.Line {
		// Create the KEY token and insert it into the queue.
		token := Token{
			Type:      KEY_TOKEN,
			StartMark: simple_key.mark,
			EndMark:   simple_key.mark,
		}
		parser.insertToken(simple_key.token_number-parser.tokens_parsed, &token)

		// In the block context, we may need to add the BLOCK-MAPPING-START token.
		if err := parser.rollIndent(simple_key.mark.Column,
			simple_key.token_number,
			BLOCK_MAPPING_START_TOKEN, simple_key.mark); err != nil {
			return err
		}

		// Remove the simple key.
		parser.simple_key_possible = false
		simple_key.required = false

		// A simple key cannot follow another simple key.
		parser.simple_key_allowed = false

	} else {
		// The ':' indicator follows a complex key.

		// In the block context, extra checks are required.
		if parser.flow_level == 0 {

			// Check if we are allowed to start a complex value.
			if !parser.simple_key_allowed {
				return formatScannerError("mapping values are not allowed in this context", parser.mark)
			}

			// Add the BLOCK-MAPPING-START token if needed.
			if err := parser.rollIndent(parser.mark.Column, -1, BLOCK_MAPPING_START_TOKEN, parser.mark); err != nil {
				return err
			}
		}

		// Simple keys after ':' are allowed in the block context.
		parser.simple_key_allowed = parser.flow_level == 0
	}

	// Consume the token.
	start_mark := parser.mark
	parser.skip()
	end_mark := parser.mark

	// Create the VALUE token and append it to the queue.
	token := Token{
		Type:      VALUE_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the ALIAS or ANCHOR token.
func (parser *Parser) fetchAnchor(typ TokenType) error {
	// An anchor or an alias could be a simple key.
	if err := parser.saveSimpleKey(); err != nil {
		return err
	}

	// A simple key cannot follow an anchor or an alias.
	parser.simple_key_allowed = false

	// Create the ALIAS or ANCHOR token and append it to the queue.
	var token Token
	if err := parser.scanAnchor(&token, typ); err != nil {
		return err
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the TAG token.
func (parser *Parser) fetchTag() error {
	// A tag could be a simple key.
	if err := parser.saveSimpleKey(); err != nil {
		return err
	}

	// A simple key cannot follow a tag.
	parser.simple_key_allowed = false

	// Create the TAG token and append it to the queue.
	var token Token
	if err := parser.scanTag(&token); err != nil {
		return err
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the SCALAR(...,literal) or SCALAR(...,folded) tokens.
func (parser *Parser) fetchBlockScalar(literal bool) error {
	// Remove any potential simple keys.
	if err := parser.removeSimpleKey(); err != nil {
		return err
	}

	// A simple key may follow a block scalar.
	parser.simple_key_allowed = true

	// Create the SCALAR token and append it to the queue.
	var token Token
	if err := parser.scanBlockScalar(&token, literal); err != nil {
		return err
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the SCALAR(...,single-quoted) or SCALAR(...,double-quoted) tokens.
func (parser *Parser) fetchFlowScalar(single bool) error {
	// A plain scalar could be a simple key.
	if err := parser.saveSimpleKey(); err != nil {
		return err
	}

	// A simple key cannot follow a flow scalar.
	parser.simple_key_allowed = false

	// Create the SCALAR token and append it to the queue.
	var token Token
	if err := parser.scanFlowScalar(&token, single); err != nil {
		return err
	}
	parser.insertToken(-1, &token)
	return nil
}

// Produce the SCALAR(...,plain) token.
func (parser *Parser) fetchPlainScalar() error {
	// A plain scalar could be a simple key.
	if err := parser.saveSimpleKey(); err != nil {
		return err
	}

	// A simple key cannot follow a flow scalar.
	parser.simple_key_allowed = false

	// Create the SCALAR token and append it to the queue.
	var token Token
	if err := parser.scanPlainScalar(&token); err != nil {
		return err
	}
	parser.insertToken(-1, &token)
	return nil
}

// Eat whitespaces and comments until the next token is found.
func (parser *Parser) scanToNextToken() error {
	scan_mark := parser.mark

	// Until the next token is not found.
	for {
		// Allow the BOM mark to start a line.
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
		if parser.mark.Column == 0 && isBOM(parser.buffer, parser.buffer_pos) {
			parser.skip()
		}

		// Eat whitespaces.
		// Tabs are allowed:
		//  - in the flow context
		//  - in the block context, but not at the beginning of the line or
		//  after '-', '?', or ':' (complex value).
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}

		for parser.buffer[parser.buffer_pos] == ' ' || ((parser.flow_level > 0 || !parser.simple_key_allowed) && parser.buffer[parser.buffer_pos] == '\t') {
			parser.skip()
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}

		// Check if we just had a line comment under a sequence entry that
		// looks more like a header to the following content. Similar to this:
		//
		// - # The comment
		//   - Some data
		//
		// If so, transform the line comment to a head comment and reposition.
		if len(parser.comments) > 0 && len(parser.tokens) > 1 {
			tokenA := parser.tokens[len(parser.tokens)-2]
			tokenB := parser.tokens[len(parser.tokens)-1]
			comment := &parser.comments[len(parser.comments)-1]
			if tokenA.Type == BLOCK_SEQUENCE_START_TOKEN && tokenB.Type == BLOCK_ENTRY_TOKEN && len(comment.Line) > 0 && !isLineBreak(parser.buffer, parser.buffer_pos) {
				// If it was in the prior line, reposition so it becomes a
				// header of the follow up token. Otherwise, keep it in place
				// so it becomes a header of the former.
				comment.Head = comment.Line
				comment.Line = nil
				if comment.StartMark.Line == parser.mark.Line-1 {
					comment.TokenMark = parser.mark
				}
			}
		}

		// Eat a comment until a line break.
		if parser.buffer[parser.buffer_pos] == '#' {
			if err := parser.scanComments(scan_mark); err != nil {
				return err
			}
		}

		// If it is a line break, eat it.
		if isLineBreak(parser.buffer, parser.buffer_pos) {
			if parser.unread < 2 {
				if err := parser.updateBuffer(2); err != nil {
					return err
				}
			}
			parser.skipLine()

			// In the block context, a new line may start a simple key.
			if parser.flow_level == 0 {
				parser.simple_key_allowed = true
			}
		} else {
			break // We have found a token.
		}
	}

	return nil
}

// Scan a YAML-DIRECTIVE or TAG-DIRECTIVE token.
//
// Scope:
//
//	%YAML    1.1    # a comment \n
//	^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
//	%TAG    !yaml!  tag:yaml.org,2002:  \n
//	^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
func (parser *Parser) scanDirective(token *Token) error {
	// Eat '%'.
	start_mark := parser.mark
	parser.skip()

	// Scan the directive name.
	var name []byte
	if err := parser.scanDirectiveName(start_mark, &name); err != nil {
		return err
	}

	// Is it a YAML directive?
	if bytes.Equal(name, []byte("YAML")) {
		// Scan the VERSION directive value.
		var major, minor int8
		if err := parser.scanVersionDirectiveValue(start_mark, &major, &minor); err != nil {
			return err
		}
		end_mark := parser.mark

		// Create a VERSION-DIRECTIVE token.
		*token = Token{
			Type:      VERSION_DIRECTIVE_TOKEN,
			StartMark: start_mark,
			EndMark:   end_mark,
			major:     major,
			minor:     minor,
		}

		// Is it a TAG directive?
	} else if bytes.Equal(name, []byte("TAG")) {
		// Scan the TAG directive value.
		var handle, prefix []byte
		if err := parser.scanTagDirectiveValue(start_mark, &handle, &prefix); err != nil {
			return err
		}
		end_mark := parser.mark

		// Create a TAG-DIRECTIVE token.
		*token = Token{
			Type:      TAG_DIRECTIVE_TOKEN,
			StartMark: start_mark,
			EndMark:   end_mark,
			Value:     handle,
			prefix:    prefix,
		}

		// Unknown directive.
	} else {
		return formatScannerErrorContext("while scanning a directive", start_mark,
			"found unknown directive name", parser.mark)
	}

	// Eat the rest of the line including any comments.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	for isBlank(parser.buffer, parser.buffer_pos) {
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	if parser.buffer[parser.buffer_pos] == '#' {
		// [Go] Discard this inline comment for the time being.
		//if !parser.ScanLineComment(start_mark) {
		//	return false
		//}
		for !isBreakOrZero(parser.buffer, parser.buffer_pos) {
			parser.skip()
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}
	}

	// Check if we are at the end of the line.
	if !isBreakOrZero(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a directive", start_mark,
			"did not find expected comment or line break", parser.mark)
	}

	// Eat a line break.
	if isLineBreak(parser.buffer, parser.buffer_pos) {
		if parser.unread < 2 {
			if err := parser.updateBuffer(2); err != nil {
				return err
			}
		}
		parser.skipLine()
	}

	return nil
}

// Scan the directive name.
//
// Scope:
//
//	%YAML   1.1     # a comment \n
//	 ^^^^
//	%TAG    !yaml!  tag:yaml.org,2002:  \n
//	 ^^^
func (parser *Parser) scanDirectiveName(start_mark Mark, name *[]byte) error {
	// Consume the directive name.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	var s []byte
	for isAlpha(parser.buffer, parser.buffer_pos) {
		s = parser.read(s)
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Check if the name is empty.
	if len(s) == 0 {
		return formatScannerErrorContext("while scanning a directive", start_mark,
			"could not find expected directive name", parser.mark)
	}

	// Check for an blank character after the name.
	if !isBlankOrZero(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a directive", start_mark,
			"found unexpected non-alphabetical character", parser.mark)
	}
	*name = s
	return nil
}

// Scan the value of VERSION-DIRECTIVE.
//
// Scope:
//
//	%YAML   1.1     # a comment \n
//	     ^^^^^^
func (parser *Parser) scanVersionDirectiveValue(start_mark Mark, major, minor *int8) error {
	// Eat whitespaces.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	for isBlank(parser.buffer, parser.buffer_pos) {
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Consume the major version number.
	if err := parser.scanVersionDirectiveNumber(start_mark, major); err != nil {
		return err
	}

	// Eat '.'.
	if parser.buffer[parser.buffer_pos] != '.' {
		return formatScannerErrorContext("while scanning a %YAML directive", start_mark,
			"did not find expected digit or '.' character", parser.mark)
	}

	parser.skip()

	// Consume the minor version number.
	if err := parser.scanVersionDirectiveNumber(start_mark, minor); err != nil {
		return err
	}
	return nil
}

const max_number_length = 2

// Scan the version number of VERSION-DIRECTIVE.
//
// Scope:
//
//	%YAML   1.1     # a comment \n
//	        ^
//	%YAML   1.1     # a comment \n
//	          ^
func (parser *Parser) scanVersionDirectiveNumber(start_mark Mark, number *int8) error {
	// Repeat while the next character is digit.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	var value, length int8
	for isDigit(parser.buffer, parser.buffer_pos) {
		// Check if the number is too long.
		length++
		if length > max_number_length {
			return formatScannerErrorContext("while scanning a %YAML directive", start_mark,
				"found extremely long version number", parser.mark)
		}
		value = value*10 + int8(asDigit(parser.buffer, parser.buffer_pos))
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Check if the number was present.
	if length == 0 {
		return formatScannerErrorContext("while scanning a %YAML directive", start_mark,
			"did not find expected version number", parser.mark)
	}
	*number = value
	return nil
}

// Scan the value of a TAG-DIRECTIVE token.
//
// Scope:
//
//	%TAG    !yaml!  tag:yaml.org,2002:  \n
//	    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
func (parser *Parser) scanTagDirectiveValue(start_mark Mark, handle, prefix *[]byte) error {
	var handle_value, prefix_value []byte

	// Eat whitespaces.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	for isBlank(parser.buffer, parser.buffer_pos) {
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Scan a handle.
	if err := parser.scanTagHandle(true, start_mark, &handle_value); err != nil {
		return err
	}

	// Expect a whitespace.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	if !isBlank(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a %TAG directive", start_mark,
			"did not find expected whitespace", parser.mark)
	}

	// Eat whitespaces.
	for isBlank(parser.buffer, parser.buffer_pos) {
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Scan a prefix (TAG directive URI - flow indicators allowed).
	if err := parser.scanTagURI(true, true, nil, start_mark, &prefix_value); err != nil {
		return err
	}

	// Expect a whitespace or line break.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	if !isBlankOrZero(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a %TAG directive", start_mark,
			"did not find expected whitespace or line break", parser.mark)
	}

	*handle = handle_value
	*prefix = prefix_value
	return nil
}

func (parser *Parser) scanAnchor(token *Token, typ TokenType) error {
	var s []byte

	// Eat the indicator character.
	start_mark := parser.mark
	parser.skip()

	// Consume the value.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	for isAnchorChar(parser.buffer, parser.buffer_pos) {
		s = parser.read(s)
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	end_mark := parser.mark

	/*
	 * Check if length of the anchor is greater than 0 and it is followed by
	 * a whitespace character or one of the indicators:
	 *
	 *      '?', ':', ',', ']', '}', '%', '@', '`'.
	 */

	if len(s) == 0 ||
		!(isBlankOrZero(parser.buffer, parser.buffer_pos) || parser.buffer[parser.buffer_pos] == '?' ||
			parser.buffer[parser.buffer_pos] == ':' || parser.buffer[parser.buffer_pos] == ',' ||
			parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '}' ||
			parser.buffer[parser.buffer_pos] == '%' || parser.buffer[parser.buffer_pos] == '@' ||
			parser.buffer[parser.buffer_pos] == '`') {
		context := "while scanning an alias"
		if typ == ANCHOR_TOKEN {
			context = "while scanning an anchor"
		}
		return formatScannerErrorContext(context, start_mark,
			"did not find expected alphabetic or numeric character", parser.mark)
	}

	// Create a token.
	*token = Token{
		Type:      typ,
		StartMark: start_mark,
		EndMark:   end_mark,
		Value:     s,
	}

	return nil
}

/*
 * Scan a TAG token.
 */

func (parser *Parser) scanTag(token *Token) error {
	var handle, suffix []byte

	start_mark := parser.mark

	// Check if the tag is in the canonical form.
	if parser.unread < 2 {
		if err := parser.updateBuffer(2); err != nil {
			return err
		}
	}

	if parser.buffer[parser.buffer_pos+1] == '<' {
		// Keep the handle as ''

		// Eat '!<'
		parser.skip()
		parser.skip()

		// Consume the tag value (verbatim tag - flow indicators allowed).
		if err := parser.scanTagURI(false, true, nil, start_mark, &suffix); err != nil {
			return err
		}

		// Check for '>' and eat it.
		if parser.buffer[parser.buffer_pos] != '>' {
			return formatScannerErrorContext("while scanning a tag", start_mark,
				"did not find the expected '>'", parser.mark)
		}

		parser.skip()
	} else {
		// The tag has either the '!suffix' or the '!handle!suffix' form.

		// First, try to scan a handle.
		if err := parser.scanTagHandle(false, start_mark, &handle); err != nil {
			return err
		}

		// Check if it is, indeed, handle.
		if handle[0] == '!' && len(handle) > 1 && handle[len(handle)-1] == '!' {
			// Scan the suffix now (short form - flow indicators not allowed).
			if err := parser.scanTagURI(false, false, nil, start_mark, &suffix); err != nil {
				return err
			}
		} else {
			// It wasn't a handle after all.  Scan the rest of the tag (short form).
			if err := parser.scanTagURI(false, false, handle, start_mark, &suffix); err != nil {
				return err
			}

			// Set the handle to '!'.
			handle = []byte{'!'}

			// A special case: the '!' tag.  Set the handle to '' and the
			// suffix to '!'.
			if len(suffix) == 0 {
				handle, suffix = suffix, handle
			}
		}
	}

	// Check the character which ends the tag.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	if !isBlankOrZero(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a tag", start_mark,
			"did not find expected whitespace or line break", parser.mark)
	}

	end_mark := parser.mark

	// Create a token.
	*token = Token{
		Type:      TAG_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
		Value:     handle,
		suffix:    suffix,
	}
	return nil
}

// Scan a tag handle.
func (parser *Parser) scanTagHandle(directive bool, start_mark Mark, handle *[]byte) error {
	// Check the initial '!' character.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	if parser.buffer[parser.buffer_pos] != '!' {
		return parser.setScannerTagError(directive,
			start_mark, "did not find expected '!'")
	}

	var s []byte

	// Copy the '!' character.
	s = parser.read(s)

	// Copy all subsequent alphabetical and numerical characters.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	for isAlpha(parser.buffer, parser.buffer_pos) {
		s = parser.read(s)
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}

	// Check if the trailing character is '!' and copy it.
	if parser.buffer[parser.buffer_pos] == '!' {
		s = parser.read(s)
	} else {
		// It's either the '!' tag or not really a tag handle.  If it's a %TAG
		// directive, it's an error.  If it's a tag token, it must be a part of URI.
		if directive && string(s) != "!" {
			return parser.setScannerTagError(directive,
				start_mark, "did not find expected '!'")
		}
	}

	*handle = s
	return nil
}

// Scan a tag URI.
// directive: true if scanning a %TAG directive URI
// verbatim: true if scanning a verbatim tag !<...> or TAG directive (flow indicators allowed)
func (parser *Parser) scanTagURI(directive bool, verbatim bool, head []byte, start_mark Mark, uri *[]byte) error {
	// size_t length = head ? strlen((char *)head) : 0
	var s []byte
	hasTag := len(head) > 0

	// Copy the head if needed.
	//
	// Note that we don't copy the leading '!' character.
	if len(head) > 1 {
		s = append(s, head[1:]...)
	}

	// Scan the tag.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	// The set of characters that may appear in URI is as follows:
	//
	//      '0'-'9', 'A'-'Z', 'a'-'z', '_', '-', ';', '/', '?', ':', '@', '&',
	//      '=', '+', '$', '.', '!', '~', '*', '\'', '(', ')', '%'.
	//
	// Note: Flow indicators (',', '[', ']', '{', '}') are only allowed in verbatim tags.
	for isTagURIChar(parser.buffer, parser.buffer_pos, verbatim) {
		// Check if it is a URI-escape sequence.
		if parser.buffer[parser.buffer_pos] == '%' {
			if err := parser.scanURIEscapes(directive, start_mark, &s); err != nil {
				return err
			}
		} else {
			s = parser.read(s)
		}
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
		hasTag = true
	}

	// Check for characters which are not allowed in tags.
	// For non-verbatim tags, if we stopped at a printable character that isn't whitespace,
	// it's an invalid tag character - give a specific error.
	// For verbatim tags, the caller will check for the expected '>' delimiter.
	if !verbatim {
		c := parser.buffer[parser.buffer_pos]
		if !isBlankOrZero(parser.buffer, parser.buffer_pos) &&
			c >= 0x20 && c <= 0x7E {
			return parser.setScannerTagError(directive, start_mark,
				fmt.Sprintf("found character '%c' that is not allowed in a YAML tag", c))
		}
	}

	if !hasTag {
		return parser.setScannerTagError(directive,
			start_mark, "did not find expected tag URI")
	}
	*uri = s
	return nil
}

// Decode an URI-escape sequence corresponding to a single UTF-8 character.
func (parser *Parser) scanURIEscapes(directive bool, start_mark Mark, s *[]byte) error {
	// Decode the required number of characters.
	w := 1024
	for w > 0 {
		// Check for a URI-escaped octet.
		if parser.unread < 3 {
			if err := parser.updateBuffer(3); err != nil {
				return err
			}
		}

		if !(parser.buffer[parser.buffer_pos] == '%' &&
			isHex(parser.buffer, parser.buffer_pos+1) &&
			isHex(parser.buffer, parser.buffer_pos+2)) {
			return parser.setScannerTagError(directive,
				start_mark, "did not find URI escaped octet")
		}

		// Get the octet.
		octet := byte((asHex(parser.buffer, parser.buffer_pos+1) << 4) + asHex(parser.buffer, parser.buffer_pos+2))

		// If it is the leading octet, determine the length of the UTF-8 sequence.
		if w == 1024 {
			w = width(octet)
			if w == 0 {
				return parser.setScannerTagError(directive,
					start_mark, "found an incorrect leading UTF-8 octet")
			}
		} else {
			// Check if the trailing octet is correct.
			if octet&0xC0 != 0x80 {
				return parser.setScannerTagError(directive,
					start_mark, "found an incorrect trailing UTF-8 octet")
			}
		}

		// Copy the octet and move the pointers.
		*s = append(*s, octet)
		parser.skip()
		parser.skip()
		parser.skip()
		w--
	}
	return nil
}

// Scan a block scalar.
func (parser *Parser) scanBlockScalar(token *Token, literal bool) error {
	// Eat the indicator '|' or '>'.
	start_mark := parser.mark
	parser.skip()

	// Scan the additional block scalar indicators.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}

	// Check for a chomping indicator.
	var chomping, increment int
	if parser.buffer[parser.buffer_pos] == '+' || parser.buffer[parser.buffer_pos] == '-' {
		// Set the chomping method and eat the indicator.
		if parser.buffer[parser.buffer_pos] == '+' {
			chomping = +1
		} else {
			chomping = -1
		}
		parser.skip()

		// Check for an indentation indicator.
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
		if isDigit(parser.buffer, parser.buffer_pos) {
			// Check that the indentation is greater than 0.
			if parser.buffer[parser.buffer_pos] == '0' {
				return formatScannerErrorContext("while scanning a block scalar", start_mark,
					"found an indentation indicator equal to 0", parser.mark)
			}

			// Get the indentation level and eat the indicator.
			increment = asDigit(parser.buffer, parser.buffer_pos)
			parser.skip()
		}

	} else if isDigit(parser.buffer, parser.buffer_pos) {
		// Do the same as above, but in the opposite order.

		if parser.buffer[parser.buffer_pos] == '0' {
			return formatScannerErrorContext("while scanning a block scalar", start_mark,
				"found an indentation indicator equal to 0", parser.mark)
		}
		increment = asDigit(parser.buffer, parser.buffer_pos)
		parser.skip()

		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
		if parser.buffer[parser.buffer_pos] == '+' || parser.buffer[parser.buffer_pos] == '-' {
			if parser.buffer[parser.buffer_pos] == '+' {
				chomping = +1
			} else {
				chomping = -1
			}
			parser.skip()
		}
	}

	// Eat whitespaces and comments to the end of the line.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	for isBlank(parser.buffer, parser.buffer_pos) {
		parser.skip()
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
	}
	if parser.buffer[parser.buffer_pos] == '#' {
		if err := parser.scanLineComment(start_mark); err != nil {
			return err
		}
		for !isBreakOrZero(parser.buffer, parser.buffer_pos) {
			parser.skip()
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}
	}

	// Check if we are at the end of the line.
	if !isBreakOrZero(parser.buffer, parser.buffer_pos) {
		return formatScannerErrorContext("while scanning a block scalar", start_mark,
			"did not find expected comment or line break", parser.mark)
	}

	// Eat a line break.
	if isLineBreak(parser.buffer, parser.buffer_pos) {
		if parser.unread < 2 {
			if err := parser.updateBuffer(2); err != nil {
				return err
			}
		}
		parser.skipLine()
	}

	end_mark := parser.mark

	// Set the indentation level if it was specified.
	var indent int
	if increment > 0 {
		if parser.indent >= 0 {
			indent = parser.indent + increment
		} else {
			indent = increment
		}
	}

	// Scan the leading line breaks and determine the indentation level if needed.
	var s, leading_break, trailing_breaks []byte
	if err := parser.scanBlockScalarBreaks(&indent, &trailing_breaks, start_mark, &end_mark); err != nil {
		return err
	}

	// Scan the block scalar content.
	if parser.unread < 1 {
		if err := parser.updateBuffer(1); err != nil {
			return err
		}
	}
	var leading_blank, trailing_blank bool
	for parser.mark.Column == indent && !isZeroChar(parser.buffer, parser.buffer_pos) {
		// We are at the beginning of a non-empty line.

		// Is it a trailing whitespace?
		trailing_blank = isBlank(parser.buffer, parser.buffer_pos)

		// Check if we need to fold the leading line break.
		if !literal && !leading_blank && !trailing_blank && len(leading_break) > 0 && leading_break[0] == '\n' {
			// Do we need to join the lines by space?
			if len(trailing_breaks) == 0 {
				s = append(s, ' ')
			}
		} else {
			s = append(s, leading_break...)
		}
		leading_break = leading_break[:0]

		// Append the remaining line breaks.
		s = append(s, trailing_breaks...)
		trailing_breaks = trailing_breaks[:0]

		// Is it a leading whitespace?
		leading_blank = isBlank(parser.buffer, parser.buffer_pos)

		// Consume the current line.
		for !isBreakOrZero(parser.buffer, parser.buffer_pos) {
			s = parser.read(s)
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}

		// Consume the line break.
		if parser.unread < 2 {
			if err := parser.updateBuffer(2); err != nil {
				return err
			}
		}

		leading_break = parser.readLine(leading_break)

		// Eat the following indentation spaces and line breaks.
		if err := parser.scanBlockScalarBreaks(&indent, &trailing_breaks, start_mark, &end_mark); err != nil {
			return err
		}
	}

	// Chomp the tail.
	if chomping != -1 {
		s = append(s, leading_break...)
	}
	if chomping == 1 {
		s = append(s, trailing_breaks...)
	}

	// Create a token.
	*token = Token{
		Type:      SCALAR_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
		Value:     s,
		Style:     LITERAL_SCALAR_STYLE,
	}
	if !literal {
		token.Style = FOLDED_SCALAR_STYLE
	}
	return nil
}

// Scan indentation spaces and line breaks for a block scalar.  Determine the
// indentation level if needed.
func (parser *Parser) scanBlockScalarBreaks(indent *int, breaks *[]byte, start_mark Mark, end_mark *Mark) error {
	*end_mark = parser.mark

	// Eat the indentation spaces and line breaks.
	max_indent := 0
	for {
		// Eat the indentation spaces.
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}
		for (*indent == 0 || parser.mark.Column < *indent) && isSpace(parser.buffer, parser.buffer_pos) {
			parser.skip()
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}
		if parser.mark.Column > max_indent {
			max_indent = parser.mark.Column
		}

		// Check for a tab character messing the indentation.
		if (*indent == 0 || parser.mark.Column < *indent) && isTab(parser.buffer, parser.buffer_pos) {
			return formatScannerErrorContext("while scanning a block scalar", start_mark,
				"found a tab character where an indentation space is expected", parser.mark)
		}

		// Have we found a non-empty line?
		if !isLineBreak(parser.buffer, parser.buffer_pos) {
			break
		}

		// Consume the line break.
		if parser.unread < 2 {
			if err := parser.updateBuffer(2); err != nil {
				return err
			}
		}
		// [Go] Should really be returning breaks instead.
		*breaks = parser.readLine(*breaks)
		*end_mark = parser.mark
	}

	// Determine the indentation level if needed.
	if *indent == 0 {
		*indent = max_indent
		if *indent < parser.indent+1 {
			*indent = parser.indent + 1
		}
		if *indent < 1 {
			*indent = 1
		}
	}
	return nil
}

// Scan a quoted scalar.
func (parser *Parser) scanFlowScalar(token *Token, single bool) error {
	// Eat the left quote.
	start_mark := parser.mark
	parser.skip()

	// Consume the content of the quoted scalar.
	var s, leading_break, trailing_breaks, whitespaces []byte
	for {
		// Check that there are no document indicators at the beginning of the line.
		if parser.unread < 4 {
			if err := parser.updateBuffer(4); err != nil {
				return err
			}
		}

		if parser.mark.Column == 0 &&
			((parser.buffer[parser.buffer_pos+0] == '-' &&
				parser.buffer[parser.buffer_pos+1] == '-' &&
				parser.buffer[parser.buffer_pos+2] == '-') ||
				(parser.buffer[parser.buffer_pos+0] == '.' &&
					parser.buffer[parser.buffer_pos+1] == '.' &&
					parser.buffer[parser.buffer_pos+2] == '.')) &&
			isBlankOrZero(parser.buffer, parser.buffer_pos+3) {
			return formatScannerErrorContext("while scanning a quoted scalar", start_mark,
				"found unexpected document indicator", parser.mark)
		}

		// Check for EOF.
		if isZeroChar(parser.buffer, parser.buffer_pos) {
			return formatScannerErrorContext("while scanning a quoted scalar", start_mark,
				"found unexpected end of stream", parser.mark)
		}

		// Consume non-blank characters.
		leading_blanks := false
		for !isBlankOrZero(parser.buffer, parser.buffer_pos) {
			if single && parser.buffer[parser.buffer_pos] == '\'' && parser.buffer[parser.buffer_pos+1] == '\'' {
				// Is is an escaped single quote.
				s = append(s, '\'')
				parser.skip()
				parser.skip()

			} else if single && parser.buffer[parser.buffer_pos] == '\'' {
				// It is a right single quote.
				break
			} else if !single && parser.buffer[parser.buffer_pos] == '"' {
				// It is a right double quote.
				break
			} else if !single && parser.buffer[parser.buffer_pos] == '\\' && isLineBreak(parser.buffer, parser.buffer_pos+1) {
				// It is an escaped line break.
				if parser.unread < 3 {
					if err := parser.updateBuffer(3); err != nil {
						return err
					}
				}
				parser.skip()
				parser.skipLine()
				leading_blanks = true
				break

			} else if !single && parser.buffer[parser.buffer_pos] == '\\' {
				// It is an escape sequence.
				code_length := 0

				// Check the escape character.
				switch parser.buffer[parser.buffer_pos+1] {
				case '0':
					s = append(s, 0)
				case 'a':
					s = append(s, '\x07')
				case 'b':
					s = append(s, '\x08')
				case 't', '\t':
					s = append(s, '\x09')
				case 'n':
					s = append(s, '\x0A')
				case 'v':
					s = append(s, '\x0B')
				case 'f':
					s = append(s, '\x0C')
				case 'r':
					s = append(s, '\x0D')
				case 'e':
					s = append(s, '\x1B')
				case ' ':
					s = append(s, '\x20')
				case '"':
					s = append(s, '"')
				case '\'':
					s = append(s, '\'')
				case '\\':
					s = append(s, '\\')
				case 'N': // NEL (#x85)
					s = append(s, '\xC2')
					s = append(s, '\x85')
				case '_': // #xA0
					s = append(s, '\xC2')
					s = append(s, '\xA0')
				case 'L': // LS (#x2028)
					s = append(s, '\xE2')
					s = append(s, '\x80')
					s = append(s, '\xA8')
				case 'P': // PS (#x2029)
					s = append(s, '\xE2')
					s = append(s, '\x80')
					s = append(s, '\xA9')
				case 'x':
					code_length = 2
				case 'u':
					code_length = 4
				case 'U':
					code_length = 8
				default:
					return formatScannerErrorContext("while scanning a quoted scalar", start_mark,
						"found unknown escape character", parser.mark)
				}

				parser.skip()
				parser.skip()

				// Consume an arbitrary escape code.
				if code_length > 0 {
					var value int

					// Scan the character value.
					if parser.unread < code_length {
						if err := parser.updateBuffer(code_length); err != nil {
							return err
						}
					}
					for k := 0; k < code_length; k++ {
						if !isHex(parser.buffer, parser.buffer_pos+k) {
							return formatScannerErrorContext("while scanning a quoted scalar", start_mark,
								"did not find expected hexadecimal number", parser.mark)
						}
						value = (value << 4) + asHex(parser.buffer, parser.buffer_pos+k)
					}

					// Check the value and write the character.
					if (value >= 0xD800 && value <= 0xDFFF) || value > 0x10FFFF {
						return formatScannerErrorContext("while scanning a quoted scalar", start_mark,
							"found invalid Unicode character escape code", parser.mark)
					}
					if value <= 0x7F {
						s = append(s, byte(value))
					} else if value <= 0x7FF {
						s = append(s, byte(0xC0+(value>>6)))
						s = append(s, byte(0x80+(value&0x3F)))
					} else if value <= 0xFFFF {
						s = append(s, byte(0xE0+(value>>12)))
						s = append(s, byte(0x80+((value>>6)&0x3F)))
						s = append(s, byte(0x80+(value&0x3F)))
					} else {
						s = append(s, byte(0xF0+(value>>18)))
						s = append(s, byte(0x80+((value>>12)&0x3F)))
						s = append(s, byte(0x80+((value>>6)&0x3F)))
						s = append(s, byte(0x80+(value&0x3F)))
					}

					// Advance the pointer.
					for k := 0; k < code_length; k++ {
						parser.skip()
					}
				}
			} else {
				// It is a non-escaped non-blank character.
				s = parser.read(s)
			}
			if parser.unread < 2 {
				if err := parser.updateBuffer(2); err != nil {
					return err
				}
			}
		}

		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}

		// Check if we are at the end of the scalar.
		if single {
			if parser.buffer[parser.buffer_pos] == '\'' {
				break
			}
		} else {
			if parser.buffer[parser.buffer_pos] == '"' {
				break
			}
		}

		// Consume blank characters.
		for isBlank(parser.buffer, parser.buffer_pos) || isLineBreak(parser.buffer, parser.buffer_pos) {
			if isBlank(parser.buffer, parser.buffer_pos) {
				// Consume a space or a tab character.
				if !leading_blanks {
					whitespaces = parser.read(whitespaces)
				} else {
					parser.skip()
				}
			} else {
				if parser.unread < 2 {
					if err := parser.updateBuffer(2); err != nil {
						return err
					}
				}

				// Check if it is a first line break.
				if !leading_blanks {
					whitespaces = whitespaces[:0]
					leading_break = parser.readLine(leading_break)
					leading_blanks = true
				} else {
					trailing_breaks = parser.readLine(trailing_breaks)
				}
			}
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}

		// Join the whitespaces or fold line breaks.
		if leading_blanks {
			// Do we need to fold line breaks?
			if len(leading_break) > 0 && leading_break[0] == '\n' {
				if len(trailing_breaks) == 0 {
					s = append(s, ' ')
				} else {
					s = append(s, trailing_breaks...)
				}
			} else {
				s = append(s, leading_break...)
				s = append(s, trailing_breaks...)
			}
			trailing_breaks = trailing_breaks[:0]
			leading_break = leading_break[:0]
		} else {
			s = append(s, whitespaces...)
			whitespaces = whitespaces[:0]
		}
	}

	// Eat the right quote.
	parser.skip()
	end_mark := parser.mark

	// Create a token.
	*token = Token{
		Type:      SCALAR_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
		Value:     s,
		Style:     SINGLE_QUOTED_SCALAR_STYLE,
	}
	if !single {
		token.Style = DOUBLE_QUOTED_SCALAR_STYLE
	}
	return nil
}

// Scan a plain scalar.
func (parser *Parser) scanPlainScalar(token *Token) error {
	var s, leading_break, trailing_breaks, whitespaces []byte
	var leading_blanks bool
	indent := parser.indent + 1

	start_mark := parser.mark
	end_mark := parser.mark

	// Consume the content of the plain scalar.
	for {
		// Check for a document indicator.
		if parser.unread < 4 {
			if err := parser.updateBuffer(4); err != nil {
				return err
			}
		}
		if parser.mark.Column == 0 &&
			((parser.buffer[parser.buffer_pos+0] == '-' &&
				parser.buffer[parser.buffer_pos+1] == '-' &&
				parser.buffer[parser.buffer_pos+2] == '-') ||
				(parser.buffer[parser.buffer_pos+0] == '.' &&
					parser.buffer[parser.buffer_pos+1] == '.' &&
					parser.buffer[parser.buffer_pos+2] == '.')) &&
			isBlankOrZero(parser.buffer, parser.buffer_pos+3) {
			break
		}

		// Check for a comment.
		if parser.buffer[parser.buffer_pos] == '#' {
			break
		}

		// Consume non-blank characters.
		for !isBlankOrZero(parser.buffer, parser.buffer_pos) {

			// Check for indicators that may end a plain scalar.
			if (parser.buffer[parser.buffer_pos] == ':' && isBlankOrZero(parser.buffer, parser.buffer_pos+1)) ||
				(parser.flow_level > 0 &&
					(parser.buffer[parser.buffer_pos] == ',' ||
						(parser.buffer[parser.buffer_pos] == '?' && isBlankOrZero(parser.buffer, parser.buffer_pos+1)) ||
						parser.buffer[parser.buffer_pos] == '[' ||
						parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '{' ||
						parser.buffer[parser.buffer_pos] == '}')) {
				break
			}

			// Check if we need to join whitespaces and breaks.
			if leading_blanks || len(whitespaces) > 0 {
				if leading_blanks {
					// Do we need to fold line breaks?
					if leading_break[0] == '\n' {
						if len(trailing_breaks) == 0 {
							s = append(s, ' ')
						} else {
							s = append(s, trailing_breaks...)
						}
					} else {
						s = append(s, leading_break...)
						s = append(s, trailing_breaks...)
					}
					trailing_breaks = trailing_breaks[:0]
					leading_break = leading_break[:0]
					leading_blanks = false
				} else {
					s = append(s, whitespaces...)
					whitespaces = whitespaces[:0]
				}
			}

			// Copy the character.
			s = parser.read(s)

			end_mark = parser.mark
			if parser.unread < 2 {
				if err := parser.updateBuffer(2); err != nil {
					return err
				}
			}
		}

		// Is it the end?
		if !(isBlank(parser.buffer, parser.buffer_pos) || isLineBreak(parser.buffer, parser.buffer_pos)) {
			break
		}

		// Consume blank characters.
		if parser.unread < 1 {
			if err := parser.updateBuffer(1); err != nil {
				return err
			}
		}

		for isBlank(parser.buffer, parser.buffer_pos) || isLineBreak(parser.buffer, parser.buffer_pos) {
			if isBlank(parser.buffer, parser.buffer_pos) {

				// Check for tab characters that abuse indentation.
				if leading_blanks && parser.mark.Column < indent && isTab(parser.buffer, parser.buffer_pos) {
					return formatScannerErrorContext("while scanning a plain scalar", start_mark,
						"found a tab character that violates indentation", parser.mark)
				}

				// Consume a space or a tab character.
				if !leading_blanks {
					whitespaces = parser.read(whitespaces)
				} else {
					parser.skip()
				}
			} else {
				if parser.unread < 2 {
					if err := parser.updateBuffer(2); err != nil {
						return err
					}
				}

				// Check if it is a first line break.
				if !leading_blanks {
					whitespaces = whitespaces[:0]
					leading_break = parser.readLine(leading_break)
					leading_blanks = true
				} else {
					trailing_breaks = parser.readLine(trailing_breaks)
				}
			}
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
		}

		// Check indentation level.
		if parser.flow_level == 0 && parser.mark.Column < indent {
			break
		}
	}

	// Create a token.
	*token = Token{
		Type:      SCALAR_TOKEN,
		StartMark: start_mark,
		EndMark:   end_mark,
		Value:     s,
		Style:     PLAIN_SCALAR_STYLE,
	}

	// Note that we change the 'simple_key_allowed' flag.
	if leading_blanks {
		parser.simple_key_allowed = true
	}
	return nil
}

func (parser *Parser) scanLineComment(token_mark Mark) error {
	if parser.newlines > 0 {
		return nil
	}

	var start_mark Mark
	var text []byte

	for peek := 0; peek < 512; peek++ {
		if parser.unread < peek+1 {
			if parser.updateBuffer(peek+1) != nil {
				break
			}
		}
		if isBlank(parser.buffer, parser.buffer_pos+peek) {
			continue
		}
		if parser.buffer[parser.buffer_pos+peek] == '#' {
			seen := parser.mark.Index + peek
			for {
				if parser.unread < 1 {
					if err := parser.updateBuffer(1); err != nil {
						return err
					}
				}
				if isBreakOrZero(parser.buffer, parser.buffer_pos) {
					if parser.mark.Index >= seen {
						break
					}
					if parser.unread < 2 {
						if err := parser.updateBuffer(2); err != nil {
							return err
						}
					}
					parser.skipLine()
				} else if parser.mark.Index >= seen {
					if len(text) == 0 {
						start_mark = parser.mark
					}
					text = parser.read(text)
				} else {
					parser.skip()
				}
			}
		}
		break
	}
	if len(text) > 0 {
		parser.comments = append(parser.comments, Comment{
			ScanMark:  token_mark,
			TokenMark: token_mark,
			StartMark: start_mark,
			EndMark:   parser.mark,
			Line:      text,
		})
	}
	return nil
}

func (parser *Parser) scanComments(scan_mark Mark) error {
	token := parser.tokens[len(parser.tokens)-1]

	if token.Type == FLOW_ENTRY_TOKEN && len(parser.tokens) > 1 {
		token = parser.tokens[len(parser.tokens)-2]
	}

	token_mark := token.StartMark
	var start_mark Mark
	next_indent := parser.indent
	if next_indent < 0 {
		next_indent = 0
	}

	recent_empty := false
	first_empty := parser.newlines <= 1

	line := parser.mark.Line
	column := parser.mark.Column

	var text []byte

	// The foot line is the place where a comment must start to
	// still be considered as a foot of the prior content.
	// If there's some content in the currently parsed line, then
	// the foot is the line below it.
	foot_line := -1
	if scan_mark.Line > 0 {
		foot_line = parser.mark.Line - parser.newlines + 1
		if parser.newlines == 0 && parser.mark.Column > 1 {
			foot_line++
		}
	}

	peek := 0
	for ; peek < 512; peek++ {
		if parser.unread < peek+1 {
			if parser.updateBuffer(peek+1) != nil {
				break
			}
		}
		column++
		if isBlank(parser.buffer, parser.buffer_pos+peek) {
			continue
		}
		c := parser.buffer[parser.buffer_pos+peek]
		close_flow := parser.flow_level > 0 && (c == ']' || c == '}')
		if close_flow || isBreakOrZero(parser.buffer, parser.buffer_pos+peek) {
			// Got line break or terminator.
			if close_flow || !recent_empty {
				if close_flow || first_empty && (start_mark.Line == foot_line && token.Type != VALUE_TOKEN || start_mark.Column-1 < next_indent) {
					// This is the first empty line and there were no empty lines before,
					// so this initial part of the comment is a foot of the prior token
					// instead of being a head for the following one. Split it up.
					// Alternatively, this might also be the last comment inside a flow
					// scope, so it must be a footer.
					if len(text) > 0 {
						if start_mark.Column-1 < next_indent {
							// If dedented it's unrelated to the prior token.
							token_mark = start_mark
						}
						parser.comments = append(parser.comments, Comment{
							ScanMark:  scan_mark,
							TokenMark: token_mark,
							StartMark: start_mark,
							EndMark:   Mark{parser.mark.Index + peek, line, column},
							Foot:      text,
						})
						scan_mark = Mark{parser.mark.Index + peek, line, column}
						token_mark = scan_mark
						text = nil
					}
				} else {
					if len(text) > 0 && parser.buffer[parser.buffer_pos+peek] != 0 {
						text = append(text, '\n')
					}
				}
			}
			if !isLineBreak(parser.buffer, parser.buffer_pos+peek) {
				break
			}
			first_empty = false
			recent_empty = true
			column = 0
			line++
			continue
		}

		if len(text) > 0 && (close_flow || column-1 < next_indent && column != start_mark.Column) {
			// The comment at the different indentation is a foot of the
			// preceding data rather than a head of the upcoming one.
			parser.comments = append(parser.comments, Comment{
				ScanMark:  scan_mark,
				TokenMark: token_mark,
				StartMark: start_mark,
				EndMark:   Mark{parser.mark.Index + peek, line, column},
				Foot:      text,
			})
			scan_mark = Mark{parser.mark.Index + peek, line, column}
			token_mark = scan_mark
			text = nil
		}

		if parser.buffer[parser.buffer_pos+peek] != '#' {
			break
		}

		if len(text) == 0 {
			start_mark = Mark{parser.mark.Index + peek, line, column}
		} else {
			text = append(text, '\n')
		}

		recent_empty = false

		// Consume until after the consumed comment line.
		seen := parser.mark.Index + peek
		for {
			if parser.unread < 1 {
				if err := parser.updateBuffer(1); err != nil {
					return err
				}
			}
			if isBreakOrZero(parser.buffer, parser.buffer_pos) {
				if parser.mark.Index >= seen {
					break
				}
				if parser.unread < 2 {
					if err := parser.updateBuffer(2); err != nil {
						return err
					}
				}
				parser.skipLine()
			} else if parser.mark.Index >= seen {
				text = parser.read(text)
			} else {
				parser.skip()
			}
		}

		peek = 0
		column = 0
		line = parser.mark.Line
		next_indent = parser.indent
		if next_indent < 0 {
			next_indent = 0
		}
	}

	if len(text) > 0 {
		parser.comments = append(parser.comments, Comment{
			ScanMark:  scan_mark,
			TokenMark: start_mark,
			StartMark: start_mark,
			EndMark:   Mark{parser.mark.Index + peek - 1, line, column},
			Head:      text,
		})
	}
	return nil
}
