// Copyright 2006-2010 Kirill Simonov
// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0 AND MIT

// Emitter stage: Generates YAML output from events.
// Handles formatting, indentation, line wrapping, and output buffering.

package libyaml

import (
	"bytes"
	"fmt"
)

// Flush the buffer if needed.
func (emitter *Emitter) flushIfNeeded() error {
	if emitter.buffer_pos+5 >= len(emitter.buffer) {
		return emitter.flush()
	}
	return nil
}

// Put a character to the output buffer.
func (emitter *Emitter) put(value byte) error {
	if emitter.buffer_pos+5 >= len(emitter.buffer) {
		if err := emitter.flush(); err != nil {
			return err
		}
	}
	emitter.buffer[emitter.buffer_pos] = value
	emitter.buffer_pos++
	emitter.column++
	return nil
}

// Put a line break to the output buffer.
func (emitter *Emitter) putLineBreak() error {
	if emitter.buffer_pos+5 >= len(emitter.buffer) {
		if err := emitter.flush(); err != nil {
			return err
		}
	}
	switch emitter.line_break {
	case CR_BREAK:
		emitter.buffer[emitter.buffer_pos] = '\r'
		emitter.buffer_pos += 1
	case LN_BREAK:
		emitter.buffer[emitter.buffer_pos] = '\n'
		emitter.buffer_pos += 1
	case CRLN_BREAK:
		emitter.buffer[emitter.buffer_pos+0] = '\r'
		emitter.buffer[emitter.buffer_pos+1] = '\n'
		emitter.buffer_pos += 2
	default:
		panic("unknown line break setting")
	}
	if emitter.column == 0 {
		emitter.space_above = true
	}
	emitter.column = 0
	emitter.line++
	// [Go] Do this here and below and drop from everywhere else (see commented lines).
	emitter.indention = true
	return nil
}

// Copy a character from a string into buffer.
func (emitter *Emitter) write(s []byte, i *int) error {
	if emitter.buffer_pos+5 >= len(emitter.buffer) {
		if err := emitter.flush(); err != nil {
			return err
		}
	}
	p := emitter.buffer_pos
	w := width(s[*i])
	switch w {
	case 4:
		emitter.buffer[p+3] = s[*i+3]
		fallthrough
	case 3:
		emitter.buffer[p+2] = s[*i+2]
		fallthrough
	case 2:
		emitter.buffer[p+1] = s[*i+1]
		fallthrough
	case 1:
		emitter.buffer[p+0] = s[*i+0]
	default:
		panic("unknown character width")
	}
	emitter.column++
	emitter.buffer_pos += w
	*i += w
	return nil
}

// Write a whole string into buffer.
func (emitter *Emitter) writeAll(s []byte) error {
	for i := 0; i < len(s); {
		if err := emitter.write(s, &i); err != nil {
			return err
		}
	}
	return nil
}

// Copy a line break character from a string into buffer.
func (emitter *Emitter) writeLineBreak(s []byte, i *int) error {
	if s[*i] == '\n' {
		if err := emitter.putLineBreak(); err != nil {
			return err
		}
		*i++
	} else {
		if err := emitter.write(s, i); err != nil {
			return err
		}
		if emitter.column == 0 {
			emitter.space_above = true
		}
		emitter.column = 0
		emitter.line++
		// [Go] Do this here and above and drop from everywhere else (see commented lines).
		emitter.indention = true
	}
	return nil
}

// Emit an event.
func (emitter *Emitter) Emit(event *Event) error {
	emitter.events = append(emitter.events, *event)
	for !emitter.needMoreEvents() {
		event := &emitter.events[emitter.events_head]
		if err := emitter.analyzeEvent(event); err != nil {
			return err
		}
		if err := emitter.stateMachine(event); err != nil {
			return err
		}
		event.Delete()
		emitter.events_head++
	}
	return nil
}

// Check if we need to accumulate more events before emitting.
//
// We accumulate extra
//   - 1 event for DOCUMENT-START
//   - 2 events for SEQUENCE-START
//   - 3 events for MAPPING-START
func (emitter *Emitter) needMoreEvents() bool {
	if emitter.events_head == len(emitter.events) {
		return true
	}
	var accumulate int
	switch emitter.events[emitter.events_head].Type {
	case DOCUMENT_START_EVENT:
		accumulate = 1
	case SEQUENCE_START_EVENT:
		accumulate = 2
	case MAPPING_START_EVENT:
		accumulate = 3
	default:
		return false
	}
	if len(emitter.events)-emitter.events_head > accumulate {
		return false
	}
	var level int
	for i := emitter.events_head; i < len(emitter.events); i++ {
		switch emitter.events[i].Type {
		case STREAM_START_EVENT, DOCUMENT_START_EVENT, SEQUENCE_START_EVENT, MAPPING_START_EVENT:
			level++
		case STREAM_END_EVENT, DOCUMENT_END_EVENT, SEQUENCE_END_EVENT, MAPPING_END_EVENT:
			level--
		}
		if level == 0 {
			return false
		}
	}
	return true
}

// Append a directive to the directives stack.
func (emitter *Emitter) appendTagDirective(value *TagDirective, allow_duplicates bool) error {
	for i := 0; i < len(emitter.tag_directives); i++ {
		if bytes.Equal(value.handle, emitter.tag_directives[i].handle) {
			if allow_duplicates {
				return nil
			}
			return EmitterError{
				Message: "duplicate %TAG directive",
			}
		}
	}

	// [Go] Do we actually need to copy this given garbage collection
	// and the lack of deallocating destructors?
	tag_copy := TagDirective{
		handle: make([]byte, len(value.handle)),
		prefix: make([]byte, len(value.prefix)),
	}
	copy(tag_copy.handle, value.handle)
	copy(tag_copy.prefix, value.prefix)
	emitter.tag_directives = append(emitter.tag_directives, tag_copy)
	return nil
}

// Increase the indentation level.
func (emitter *Emitter) increaseIndentCompact(flow, indentless bool, compact_seq bool) error {
	emitter.indents = append(emitter.indents, emitter.indent)
	if emitter.indent < 0 {
		if flow {
			emitter.indent = emitter.BestIndent
		} else {
			emitter.indent = 0
		}
	} else if !indentless {
		// [Go] This was changed so that indentations are more regular.
		if emitter.states[len(emitter.states)-1] == EMIT_BLOCK_SEQUENCE_ITEM_STATE {
			// The first indent inside a sequence will just skip the "- " indicator.
			emitter.indent += 2
		} else {
			// Everything else aligns to the chosen indentation.
			emitter.indent = emitter.BestIndent * ((emitter.indent + emitter.BestIndent) / emitter.BestIndent)
			if compact_seq {
				// The value compact_seq passed in is almost always set to `false` when this function is called,
				// except when we are dealing with sequence nodes. So this gets triggered to subtract 2 only when we
				// are increasing the indent to account for sequence nodes, which will be correct because we need to
				// subtract 2 to account for the - at the beginning of the sequence node.
				emitter.indent = emitter.indent - 2
			}
		}
	}
	return nil
}

// State dispatcher.
func (emitter *Emitter) stateMachine(event *Event) error {
	switch emitter.state {
	default:
	case EMIT_STREAM_START_STATE:
		return emitter.emitStreamStart(event)

	case EMIT_FIRST_DOCUMENT_START_STATE:
		return emitter.emitDocumentStart(event, true)

	case EMIT_DOCUMENT_START_STATE:
		return emitter.emitDocumentStart(event, false)

	case EMIT_DOCUMENT_CONTENT_STATE:
		return emitter.emitDocumentContent(event)

	case EMIT_DOCUMENT_END_STATE:
		return emitter.emitDocumentEnd(event)

	case EMIT_FLOW_SEQUENCE_FIRST_ITEM_STATE:
		return emitter.emitFlowSequenceItem(event, true, false)

	case EMIT_FLOW_SEQUENCE_TRAIL_ITEM_STATE:
		return emitter.emitFlowSequenceItem(event, false, true)

	case EMIT_FLOW_SEQUENCE_ITEM_STATE:
		return emitter.emitFlowSequenceItem(event, false, false)

	case EMIT_FLOW_MAPPING_FIRST_KEY_STATE:
		return emitter.emitFlowMappingKey(event, true, false)

	case EMIT_FLOW_MAPPING_TRAIL_KEY_STATE:
		return emitter.emitFlowMappingKey(event, false, true)

	case EMIT_FLOW_MAPPING_KEY_STATE:
		return emitter.emitFlowMappingKey(event, false, false)

	case EMIT_FLOW_MAPPING_SIMPLE_VALUE_STATE:
		return emitter.emitFlowMappingValue(event, true)

	case EMIT_FLOW_MAPPING_VALUE_STATE:
		return emitter.emitFlowMappingValue(event, false)

	case EMIT_BLOCK_SEQUENCE_FIRST_ITEM_STATE:
		return emitter.emitBlockSequenceItem(event, true)

	case EMIT_BLOCK_SEQUENCE_ITEM_STATE:
		return emitter.emitBlockSequenceItem(event, false)

	case EMIT_BLOCK_MAPPING_FIRST_KEY_STATE:
		return emitter.emitBlockMappingKey(event, true)

	case EMIT_BLOCK_MAPPING_KEY_STATE:
		return emitter.emitBlockMappingKey(event, false)

	case EMIT_BLOCK_MAPPING_SIMPLE_VALUE_STATE:
		return emitter.emitBlockMappingValue(event, true)

	case EMIT_BLOCK_MAPPING_VALUE_STATE:
		return emitter.emitBlockMappingValue(event, false)

	case EMIT_END_STATE:
		return EmitterError{
			Message: "expected nothing after STREAM-END",
		}
	}
	panic("invalid emitter state")
}

// Expect STREAM-START.
func (emitter *Emitter) emitStreamStart(event *Event) error {
	if event.Type != STREAM_START_EVENT {
		return EmitterError{
			Message: "expected STREAM-START",
		}
	}
	if emitter.encoding == ANY_ENCODING {
		emitter.encoding = event.encoding
		if emitter.encoding == ANY_ENCODING {
			emitter.encoding = UTF8_ENCODING
		}
	}
	if emitter.BestIndent < 2 || emitter.BestIndent > 9 {
		emitter.BestIndent = 2
	}
	if emitter.best_width >= 0 && emitter.best_width <= emitter.BestIndent*2 {
		emitter.best_width = 80
	}
	if emitter.best_width < 0 {
		emitter.best_width = 1<<31 - 1
	}
	if emitter.line_break == ANY_BREAK {
		emitter.line_break = LN_BREAK
	}

	emitter.indent = -1
	emitter.line = 0
	emitter.column = 0
	emitter.whitespace = true
	emitter.indention = true
	emitter.space_above = true
	emitter.foot_indent = -1

	if emitter.encoding != UTF8_ENCODING {
		if err := emitter.writeBom(); err != nil {
			return err
		}
	}
	emitter.state = EMIT_FIRST_DOCUMENT_START_STATE
	return nil
}

// Expect DOCUMENT-START or STREAM-END.
func (emitter *Emitter) emitDocumentStart(event *Event, first bool) error {
	if event.Type == DOCUMENT_START_EVENT {

		if event.versionDirective != nil {
			if err := emitter.analyzeVersionDirective(event.versionDirective); err != nil {
				return err
			}
		}

		for i := 0; i < len(event.tagDirectives); i++ {
			tag_directive := &event.tagDirectives[i]
			if err := emitter.analyzeTagDirective(tag_directive); err != nil {
				return err
			}
			if err := emitter.appendTagDirective(tag_directive, false); err != nil {
				return err
			}
		}

		for i := 0; i < len(default_tag_directives); i++ {
			tag_directive := &default_tag_directives[i]
			if err := emitter.appendTagDirective(tag_directive, true); err != nil {
				return err
			}
		}

		implicit := event.Implicit
		if !first || emitter.canonical {
			implicit = false
		}

		if emitter.OpenEnded && (event.versionDirective != nil || len(event.tagDirectives) > 0) {
			if err := emitter.writeIndicator([]byte("..."), true, false, false); err != nil {
				return err
			}
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}

		if event.versionDirective != nil {
			implicit = false
			if err := emitter.writeIndicator([]byte("%YAML"), true, false, false); err != nil {
				return err
			}
			if err := emitter.writeIndicator([]byte("1.1"), true, false, false); err != nil {
				return err
			}
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}

		if len(event.tagDirectives) > 0 {
			implicit = false
			for i := 0; i < len(event.tagDirectives); i++ {
				tag_directive := &event.tagDirectives[i]
				if err := emitter.writeIndicator([]byte("%TAG"), true, false, false); err != nil {
					return err
				}
				if err := emitter.writeTagHandle(tag_directive.handle); err != nil {
					return err
				}
				if err := emitter.writeTagContent(tag_directive.prefix, true); err != nil {
					return err
				}
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
		}

		if emitter.checkEmptyDocument() {
			implicit = false
		}
		if !implicit {
			if err := emitter.writeIndent(); err != nil {
				return err
			}
			if err := emitter.writeIndicator([]byte("---"), true, false, false); err != nil {
				return err
			}
			if emitter.canonical || true {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
		}

		if len(emitter.HeadComment) > 0 {
			if err := emitter.processHeadComment(); err != nil {
				return err
			}
			if err := emitter.putLineBreak(); err != nil {
				return err
			}
		}

		emitter.state = EMIT_DOCUMENT_CONTENT_STATE
		return nil
	}

	if event.Type == STREAM_END_EVENT {
		if emitter.OpenEnded {
			if err := emitter.writeIndicator([]byte("..."), true, false, false); err != nil {
				return err
			}
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}
		if err := emitter.flush(); err != nil {
			return err
		}
		emitter.state = EMIT_END_STATE
		return nil
	}

	return EmitterError{
		Message: "expected DOCUMENT-START or STREAM-END",
	}
}

// emitter preserves the original signature and delegates to
// increaseIndentCompact without compact-sequence indentation
func (emitter *Emitter) increaseIndent(flow, indentless bool) error {
	return emitter.increaseIndentCompact(flow, indentless, false)
}

// processLineComment preserves the original signature and delegates to
// processLineCommentLinebreak passing false for linebreak
func (emitter *Emitter) processLineComment() error {
	return emitter.processLineCommentLinebreak(false)
}

// Expect the root node.
func (emitter *Emitter) emitDocumentContent(event *Event) error {
	emitter.states = append(emitter.states, EMIT_DOCUMENT_END_STATE)

	if err := emitter.processHeadComment(); err != nil {
		return err
	}
	if err := emitter.emitNode(event, true, false, false, false); err != nil {
		return err
	}
	if err := emitter.processLineComment(); err != nil {
		return err
	}
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	return nil
}

// Expect DOCUMENT-END.
func (emitter *Emitter) emitDocumentEnd(event *Event) error {
	if event.Type != DOCUMENT_END_EVENT {
		return EmitterError{
			Message: "expected DOCUMENT-END",
		}
	}
	// [Go] Force document foot separation.
	emitter.foot_indent = 0
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	emitter.foot_indent = -1
	if err := emitter.writeIndent(); err != nil {
		return err
	}
	if !event.Implicit {
		// [Go] Allocate the slice elsewhere.
		if err := emitter.writeIndicator([]byte("..."), true, false, false); err != nil {
			return err
		}
		if err := emitter.writeIndent(); err != nil {
			return err
		}
	}
	if err := emitter.flush(); err != nil {
		return err
	}
	emitter.state = EMIT_DOCUMENT_START_STATE
	emitter.tag_directives = emitter.tag_directives[:0]
	return nil
}

// Expect a flow item node.
func (emitter *Emitter) emitFlowSequenceItem(event *Event, first, trail bool) error {
	if first {
		if err := emitter.writeIndicator([]byte{'['}, true, true, false); err != nil {
			return err
		}
		if err := emitter.increaseIndent(true, false); err != nil {
			return err
		}
		emitter.flow_level++
	}

	if event.Type == SEQUENCE_END_EVENT {
		if emitter.canonical && !first && !trail {
			if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
				return err
			}
		}
		emitter.flow_level--
		emitter.indent = emitter.indents[len(emitter.indents)-1]
		emitter.indents = emitter.indents[:len(emitter.indents)-1]
		if emitter.column == 0 || emitter.canonical && !first {
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}
		if err := emitter.writeIndicator([]byte{']'}, false, false, false); err != nil {
			return err
		}
		if err := emitter.processLineComment(); err != nil {
			return err
		}
		if err := emitter.processFootComment(); err != nil {
			return err
		}
		emitter.state = emitter.states[len(emitter.states)-1]
		emitter.states = emitter.states[:len(emitter.states)-1]

		return nil
	}

	if !first && !trail {
		if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
			return err
		}
	}

	if err := emitter.processHeadComment(); err != nil {
		return err
	}
	if emitter.column == 0 {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
	}

	if emitter.canonical || emitter.column > emitter.best_width {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
	}
	if len(emitter.LineComment)+len(emitter.FootComment)+len(emitter.TailComment) > 0 {
		emitter.states = append(emitter.states, EMIT_FLOW_SEQUENCE_TRAIL_ITEM_STATE)
	} else {
		emitter.states = append(emitter.states, EMIT_FLOW_SEQUENCE_ITEM_STATE)
	}
	if err := emitter.emitNode(event, false, true, false, false); err != nil {
		return err
	}
	if len(emitter.LineComment)+len(emitter.FootComment)+len(emitter.TailComment) > 0 {
		if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
			return err
		}
	}
	if err := emitter.processLineComment(); err != nil {
		return err
	}
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	return nil
}

// Expect a flow key node.
func (emitter *Emitter) emitFlowMappingKey(event *Event, first, trail bool) error {
	if first {
		if err := emitter.writeIndicator([]byte{'{'}, true, true, false); err != nil {
			return err
		}
		if err := emitter.increaseIndent(true, false); err != nil {
			return err
		}
		emitter.flow_level++
	}

	if event.Type == MAPPING_END_EVENT {
		if (emitter.canonical || len(emitter.HeadComment)+len(emitter.FootComment)+len(emitter.TailComment) > 0) && !first && !trail {
			if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
				return err
			}
		}
		if err := emitter.processHeadComment(); err != nil {
			return err
		}
		emitter.flow_level--
		emitter.indent = emitter.indents[len(emitter.indents)-1]
		emitter.indents = emitter.indents[:len(emitter.indents)-1]
		if emitter.canonical && !first {
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}
		if err := emitter.writeIndicator([]byte{'}'}, false, false, false); err != nil {
			return err
		}
		if err := emitter.processLineComment(); err != nil {
			return err
		}
		if err := emitter.processFootComment(); err != nil {
			return err
		}
		emitter.state = emitter.states[len(emitter.states)-1]
		emitter.states = emitter.states[:len(emitter.states)-1]
		return nil
	}

	if !first && !trail {
		if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
			return err
		}
	}

	if err := emitter.processHeadComment(); err != nil {
		return err
	}

	if emitter.column == 0 {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
	}

	if emitter.canonical || emitter.column > emitter.best_width {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
	}

	if !emitter.canonical && emitter.checkSimpleKey() {
		emitter.states = append(emitter.states, EMIT_FLOW_MAPPING_SIMPLE_VALUE_STATE)
		return emitter.emitNode(event, false, false, true, true)
	}
	if err := emitter.writeIndicator([]byte{'?'}, true, false, false); err != nil {
		return err
	}
	emitter.states = append(emitter.states, EMIT_FLOW_MAPPING_VALUE_STATE)
	return emitter.emitNode(event, false, false, true, false)
}

// Expect a flow value node.
func (emitter *Emitter) emitFlowMappingValue(event *Event, simple bool) error {
	if simple {
		if err := emitter.writeIndicator([]byte{':'}, false, false, false); err != nil {
			return err
		}
	} else {
		if emitter.canonical || emitter.column > emitter.best_width {
			if err := emitter.writeIndent(); err != nil {
				return err
			}
		}
		if err := emitter.writeIndicator([]byte{':'}, true, false, false); err != nil {
			return err
		}
	}
	if len(emitter.LineComment)+len(emitter.FootComment)+len(emitter.TailComment) > 0 {
		emitter.states = append(emitter.states, EMIT_FLOW_MAPPING_TRAIL_KEY_STATE)
	} else {
		emitter.states = append(emitter.states, EMIT_FLOW_MAPPING_KEY_STATE)
	}
	if err := emitter.emitNode(event, false, false, true, false); err != nil {
		return err
	}
	if len(emitter.LineComment)+len(emitter.FootComment)+len(emitter.TailComment) > 0 {
		if err := emitter.writeIndicator([]byte{','}, false, false, false); err != nil {
			return err
		}
	}
	if err := emitter.processLineComment(); err != nil {
		return err
	}
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	return nil
}

// Expect a block item node.
func (emitter *Emitter) emitBlockSequenceItem(event *Event, first bool) error {
	if first {
		// emitter.mapping context tells us if we are currently in a mapping context.
		// emitter.column tells us which column we are in the yaml output. 0 is the first char of the column.
		// emitter.indentation tells us if the last character was an indentation character.
		// emitter.compact_sequence_indent tells us if '- ' is considered part of the indentation for sequence elements.
		// So, `seq` means that we are in a mapping context, and we are either at the first char of the column or
		//  the last character was not an indentation character, and we consider '- ' part of the indentation
		//  for sequence elements.
		seq := emitter.mapping_context && (emitter.column == 0 || !emitter.indention) &&
			emitter.CompactSequenceIndent
		if err := emitter.increaseIndentCompact(false, false, seq); err != nil {
			return err
		}
	}
	if event.Type == SEQUENCE_END_EVENT {
		emitter.indent = emitter.indents[len(emitter.indents)-1]
		emitter.indents = emitter.indents[:len(emitter.indents)-1]
		emitter.state = emitter.states[len(emitter.states)-1]
		emitter.states = emitter.states[:len(emitter.states)-1]
		return nil
	}
	if err := emitter.processHeadComment(); err != nil {
		return err
	}
	if err := emitter.writeIndent(); err != nil {
		return err
	}
	if err := emitter.writeIndicator([]byte{'-'}, true, false, true); err != nil {
		return err
	}
	emitter.states = append(emitter.states, EMIT_BLOCK_SEQUENCE_ITEM_STATE)
	if err := emitter.emitNode(event, false, true, false, false); err != nil {
		return err
	}
	if err := emitter.processLineComment(); err != nil {
		return err
	}
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	return nil
}

// Expect a block key node.
func (emitter *Emitter) emitBlockMappingKey(event *Event, first bool) error {
	if first {
		if err := emitter.increaseIndent(false, false); err != nil {
			return err
		}
	}
	if err := emitter.processHeadComment(); err != nil {
		return err
	}
	if event.Type == MAPPING_END_EVENT {
		emitter.indent = emitter.indents[len(emitter.indents)-1]
		emitter.indents = emitter.indents[:len(emitter.indents)-1]
		emitter.state = emitter.states[len(emitter.states)-1]
		emitter.states = emitter.states[:len(emitter.states)-1]
		return nil
	}
	if err := emitter.writeIndent(); err != nil {
		return err
	}
	if len(emitter.LineComment) > 0 {
		// [Go] A line comment was provided for the key. That's unusual as the
		//      scanner associates line comments with the value. Either way,
		//      save the line comment and render it appropriately later.
		emitter.key_line_comment = emitter.LineComment
		emitter.LineComment = nil
	}
	if emitter.checkSimpleKey() {
		emitter.states = append(emitter.states, EMIT_BLOCK_MAPPING_SIMPLE_VALUE_STATE)
		if err := emitter.emitNode(event, false, false, true, true); err != nil {
			return err
		}

		if event.Type == ALIAS_EVENT {
			// make sure there's a space after the alias
			return emitter.put(' ')
		}

		return nil
	}
	if err := emitter.writeIndicator([]byte{'?'}, true, false, true); err != nil {
		return err
	}
	emitter.states = append(emitter.states, EMIT_BLOCK_MAPPING_VALUE_STATE)
	return emitter.emitNode(event, false, false, true, false)
}

// Expect a block value node.
func (emitter *Emitter) emitBlockMappingValue(event *Event, simple bool) error {
	if simple {
		if err := emitter.writeIndicator([]byte{':'}, false, false, false); err != nil {
			return err
		}
	} else {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
		if err := emitter.writeIndicator([]byte{':'}, true, false, true); err != nil {
			return err
		}
	}
	if len(emitter.key_line_comment) > 0 {
		// [Go] Line comments are generally associated with the value, but when there's
		//      no value on the same line as a mapping key they end up attached to the
		//      key itself.
		if event.Type == SCALAR_EVENT {
			if len(emitter.LineComment) == 0 {
				// A scalar is coming and it has no line comments by itself yet,
				// so just let it handle the line comment as usual. If it has a
				// line comment, we can't have both so the one from the key is lost.
				emitter.LineComment = emitter.key_line_comment
				emitter.key_line_comment = nil
			}
		} else if event.SequenceStyle() != FLOW_SEQUENCE_STYLE && (event.Type == MAPPING_START_EVENT || event.Type == SEQUENCE_START_EVENT) {
			// An indented block follows, so write the comment right now.
			emitter.LineComment, emitter.key_line_comment = emitter.key_line_comment, emitter.LineComment
			if err := emitter.processLineComment(); err != nil {
				return err
			}
			emitter.LineComment, emitter.key_line_comment = emitter.key_line_comment, emitter.LineComment
		}
	}
	emitter.states = append(emitter.states, EMIT_BLOCK_MAPPING_KEY_STATE)
	if err := emitter.emitNode(event, false, false, true, false); err != nil {
		return err
	}
	if err := emitter.processLineComment(); err != nil {
		return err
	}
	if err := emitter.processFootComment(); err != nil {
		return err
	}
	return nil
}

func (emitter *Emitter) silentNilEvent(event *Event) bool {
	return event.Type == SCALAR_EVENT && event.Implicit && !emitter.canonical && len(emitter.scalar_data.value) == 0
}

// Expect a node.
func (emitter *Emitter) emitNode(event *Event,
	root bool, sequence bool, mapping bool, simple_key bool,
) error {
	emitter.root_context = root
	emitter.sequence_context = sequence
	emitter.mapping_context = mapping
	emitter.simple_key_context = simple_key

	switch event.Type {
	case ALIAS_EVENT:
		return emitter.emitAlias(event)
	case SCALAR_EVENT:
		return emitter.emitScalar(event)
	case SEQUENCE_START_EVENT:
		return emitter.emitSequenceStart(event)
	case MAPPING_START_EVENT:
		return emitter.emitMappingStart(event)
	default:
		return EmitterError{
			Message: fmt.Sprintf("expected SCALAR, SEQUENCE-START, MAPPING-START, or ALIAS, but got %v", event.Type),
		}
	}
}

// Expect ALIAS.
func (emitter *Emitter) emitAlias(event *Event) error {
	if err := emitter.processAnchor(); err != nil {
		return err
	}
	emitter.state = emitter.states[len(emitter.states)-1]
	emitter.states = emitter.states[:len(emitter.states)-1]
	return nil
}

// requiredQuoteStyle returns the appropriate quote style based on the
// emitter's quotePreference setting.
func (emitter *Emitter) requiredQuoteStyle() ScalarStyle {
	if emitter.quotePreference == QuoteDouble {
		return DOUBLE_QUOTED_SCALAR_STYLE
	}
	return SINGLE_QUOTED_SCALAR_STYLE
}

// Expect SCALAR.
func (emitter *Emitter) emitScalar(event *Event) error {
	if err := emitter.selectScalarStyle(event); err != nil {
		return err
	}
	if err := emitter.processAnchor(); err != nil {
		return err
	}
	if err := emitter.processTag(); err != nil {
		return err
	}
	if err := emitter.increaseIndent(true, false); err != nil {
		return err
	}
	if err := emitter.processScalar(); err != nil {
		return err
	}
	emitter.indent = emitter.indents[len(emitter.indents)-1]
	emitter.indents = emitter.indents[:len(emitter.indents)-1]
	emitter.state = emitter.states[len(emitter.states)-1]
	emitter.states = emitter.states[:len(emitter.states)-1]
	return nil
}

// Expect SEQUENCE-START.
func (emitter *Emitter) emitSequenceStart(event *Event) error {
	if err := emitter.processAnchor(); err != nil {
		return err
	}
	if err := emitter.processTag(); err != nil {
		return err
	}
	if emitter.flow_level > 0 || emitter.canonical || event.SequenceStyle() == FLOW_SEQUENCE_STYLE ||
		emitter.checkEmptySequence() {
		emitter.state = EMIT_FLOW_SEQUENCE_FIRST_ITEM_STATE
	} else {
		emitter.state = EMIT_BLOCK_SEQUENCE_FIRST_ITEM_STATE
	}
	return nil
}

// Expect MAPPING-START.
func (emitter *Emitter) emitMappingStart(event *Event) error {
	if err := emitter.processAnchor(); err != nil {
		return err
	}
	if err := emitter.processTag(); err != nil {
		return err
	}
	if emitter.flow_level > 0 || emitter.canonical || event.MappingStyle() == FLOW_MAPPING_STYLE ||
		emitter.checkEmptyMapping() {
		emitter.state = EMIT_FLOW_MAPPING_FIRST_KEY_STATE
	} else {
		emitter.state = EMIT_BLOCK_MAPPING_FIRST_KEY_STATE
	}
	return nil
}

// Check if the document content is an empty scalar.
func (emitter *Emitter) checkEmptyDocument() bool {
	return false // [Go] Huh?
}

// Check if the next events represent an empty sequence.
func (emitter *Emitter) checkEmptySequence() bool {
	if len(emitter.events)-emitter.events_head < 2 {
		return false
	}
	return emitter.events[emitter.events_head].Type == SEQUENCE_START_EVENT &&
		emitter.events[emitter.events_head+1].Type == SEQUENCE_END_EVENT
}

// Check if the next events represent an empty mapping.
func (emitter *Emitter) checkEmptyMapping() bool {
	if len(emitter.events)-emitter.events_head < 2 {
		return false
	}
	return emitter.events[emitter.events_head].Type == MAPPING_START_EVENT &&
		emitter.events[emitter.events_head+1].Type == MAPPING_END_EVENT
}

// Check if the next node can be expressed as a simple key.
func (emitter *Emitter) checkSimpleKey() bool {
	length := 0
	switch emitter.events[emitter.events_head].Type {
	case ALIAS_EVENT:
		length += len(emitter.anchor_data.anchor)
	case SCALAR_EVENT:
		if emitter.scalar_data.multiline {
			return false
		}
		length += len(emitter.anchor_data.anchor) +
			len(emitter.tag_data.handle) +
			len(emitter.tag_data.suffix) +
			len(emitter.scalar_data.value)
	case SEQUENCE_START_EVENT:
		if !emitter.checkEmptySequence() {
			return false
		}
		length += len(emitter.anchor_data.anchor) +
			len(emitter.tag_data.handle) +
			len(emitter.tag_data.suffix)
	case MAPPING_START_EVENT:
		if !emitter.checkEmptyMapping() {
			return false
		}
		length += len(emitter.anchor_data.anchor) +
			len(emitter.tag_data.handle) +
			len(emitter.tag_data.suffix)
	default:
		return false
	}
	return length <= 128
}

// Determine an acceptable scalar style.
func (emitter *Emitter) selectScalarStyle(event *Event) error {
	no_tag := len(emitter.tag_data.handle) == 0 && len(emitter.tag_data.suffix) == 0
	if no_tag && !event.Implicit && !event.quoted_implicit {
		return EmitterError{
			Message: "neither tag nor implicit flags are specified",
		}
	}

	style := event.ScalarStyle()
	if style == ANY_SCALAR_STYLE {
		style = PLAIN_SCALAR_STYLE
	}
	if emitter.canonical {
		style = DOUBLE_QUOTED_SCALAR_STYLE
	}
	if emitter.simple_key_context && emitter.scalar_data.multiline {
		style = DOUBLE_QUOTED_SCALAR_STYLE
	}

	if style == PLAIN_SCALAR_STYLE {
		if emitter.flow_level > 0 && !emitter.scalar_data.flow_plain_allowed ||
			emitter.flow_level == 0 && !emitter.scalar_data.block_plain_allowed {
			style = emitter.requiredQuoteStyle()
		}
		if len(emitter.scalar_data.value) == 0 && (emitter.flow_level > 0 || emitter.simple_key_context) {
			style = emitter.requiredQuoteStyle()
		}
		if no_tag && !event.Implicit {
			style = emitter.requiredQuoteStyle()
		}
	}
	if style == SINGLE_QUOTED_SCALAR_STYLE {
		if !emitter.scalar_data.single_quoted_allowed {
			style = DOUBLE_QUOTED_SCALAR_STYLE
		}
	}
	if style == LITERAL_SCALAR_STYLE || style == FOLDED_SCALAR_STYLE {
		if !emitter.scalar_data.block_allowed || emitter.flow_level > 0 || emitter.simple_key_context {
			style = DOUBLE_QUOTED_SCALAR_STYLE
		}
	}

	if no_tag && !event.quoted_implicit && style != PLAIN_SCALAR_STYLE {
		emitter.tag_data.handle = []byte{'!'}
	}
	emitter.scalar_data.style = style
	return nil
}

// Write an anchor.
func (emitter *Emitter) processAnchor() error {
	if emitter.anchor_data.anchor == nil {
		return nil
	}
	c := []byte{'&'}
	if emitter.anchor_data.alias {
		c[0] = '*'
	}
	if err := emitter.writeIndicator(c, true, false, false); err != nil {
		return err
	}
	return emitter.writeAnchor(emitter.anchor_data.anchor)
}

// Write a tag.
func (emitter *Emitter) processTag() error {
	if len(emitter.tag_data.handle) == 0 && len(emitter.tag_data.suffix) == 0 {
		return nil
	}
	if len(emitter.tag_data.handle) > 0 {
		if err := emitter.writeTagHandle(emitter.tag_data.handle); err != nil {
			return err
		}
		if len(emitter.tag_data.suffix) > 0 {
			if err := emitter.writeTagContent(emitter.tag_data.suffix, false); err != nil {
				return err
			}
		}
	} else {
		// [Go] Allocate these slices elsewhere.
		if err := emitter.writeIndicator([]byte("!<"), true, false, false); err != nil {
			return err
		}
		if err := emitter.writeTagContent(emitter.tag_data.suffix, false); err != nil {
			return err
		}
		if err := emitter.writeIndicator([]byte{'>'}, false, false, false); err != nil {
			return err
		}
	}
	return nil
}

// Write a scalar.
func (emitter *Emitter) processScalar() error {
	switch emitter.scalar_data.style {
	case PLAIN_SCALAR_STYLE:
		return emitter.writePlainScalar(emitter.scalar_data.value, !emitter.simple_key_context)

	case SINGLE_QUOTED_SCALAR_STYLE:
		return emitter.writeSingleQuotedScalar(emitter.scalar_data.value, !emitter.simple_key_context)

	case DOUBLE_QUOTED_SCALAR_STYLE:
		return emitter.writeDoubleQuotedScalar(emitter.scalar_data.value, !emitter.simple_key_context)

	case LITERAL_SCALAR_STYLE:
		return emitter.writeLiteralScalar(emitter.scalar_data.value)

	case FOLDED_SCALAR_STYLE:
		return emitter.writeFoldedScalar(emitter.scalar_data.value)
	}
	panic("unknown scalar style")
}

// Write a head comment.
func (emitter *Emitter) processHeadComment() error {
	if len(emitter.TailComment) > 0 {
		if err := emitter.writeIndent(); err != nil {
			return err
		}
		if err := emitter.writeComment(emitter.TailComment); err != nil {
			return err
		}
		emitter.TailComment = emitter.TailComment[:0]
		emitter.foot_indent = emitter.indent
		if emitter.foot_indent < 0 {
			emitter.foot_indent = 0
		}
	}

	if len(emitter.HeadComment) == 0 {
		return nil
	}
	if err := emitter.writeIndent(); err != nil {
		return err
	}
	if err := emitter.writeComment(emitter.HeadComment); err != nil {
		return err
	}
	emitter.HeadComment = emitter.HeadComment[:0]
	return nil
}

// Write an line comment.
func (emitter *Emitter) processLineCommentLinebreak(linebreak bool) error {
	if len(emitter.LineComment) == 0 {
		// The next 3 lines are needed to resolve an issue with leading newlines
		// See https://github.com/go-yaml/yaml/issues/755
		// When linebreak is set to true, put_break will be called and will add
		// the needed newline.
		if linebreak {
			if err := emitter.putLineBreak(); err != nil {
				return err
			}
		}
		return nil
	}
	if !emitter.whitespace {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}
	if err := emitter.writeComment(emitter.LineComment); err != nil {
		return err
	}
	emitter.LineComment = emitter.LineComment[:0]
	return nil
}

// Write a foot comment.
func (emitter *Emitter) processFootComment() error {
	if len(emitter.FootComment) == 0 {
		return nil
	}
	if err := emitter.writeIndent(); err != nil {
		return err
	}
	if err := emitter.writeComment(emitter.FootComment); err != nil {
		return err
	}
	emitter.FootComment = emitter.FootComment[:0]
	emitter.foot_indent = emitter.indent
	if emitter.foot_indent < 0 {
		emitter.foot_indent = 0
	}
	return nil
}

// Check if a %YAML directive is valid.
func (emitter *Emitter) analyzeVersionDirective(version_directive *VersionDirective) error {
	if version_directive.major != 1 || version_directive.minor != 1 {
		return EmitterError{
			Message: "incompatible %YAML directive",
		}
	}
	return nil
}

// Check if a %TAG directive is valid.
func (emitter *Emitter) analyzeTagDirective(tag_directive *TagDirective) error {
	handle := tag_directive.handle
	prefix := tag_directive.prefix
	if len(handle) == 0 {
		return EmitterError{
			Message: "tag handle must not be empty",
		}
	}
	if handle[0] != '!' {
		return EmitterError{
			Message: "tag handle must start with '!'",
		}
	}
	if handle[len(handle)-1] != '!' {
		return EmitterError{
			Message: "tag handle must end with '!'",
		}
	}
	for i := 1; i < len(handle)-1; i += width(handle[i]) {
		if !isAlpha(handle, i) {
			return EmitterError{
				Message: "tag handle must contain alphanumerical characters only",
			}
		}
	}
	if len(prefix) == 0 {
		return EmitterError{
			Message: "tag prefix must not be empty",
		}
	}
	return nil
}

// Check if an anchor is valid.
func (emitter *Emitter) analyzeAnchor(anchor []byte, alias bool) error {
	if len(anchor) == 0 {
		problem := "anchor value must not be empty"
		if alias {
			problem = "alias value must not be empty"
		}
		return EmitterError{
			Message: problem,
		}
	}
	for i := 0; i < len(anchor); i += width(anchor[i]) {
		if !isAnchorChar(anchor, i) {
			problem := "anchor value must contain valid characters only"
			if alias {
				problem = "alias value must contain valid characters only"
			}
			return EmitterError{
				Message: problem,
			}
		}
	}
	emitter.anchor_data.anchor = anchor
	emitter.anchor_data.alias = alias
	return nil
}

// Check if a tag is valid.
func (emitter *Emitter) analyzeTag(tag []byte) error {
	if len(tag) == 0 {
		return EmitterError{
			Message: "tag value must not be empty",
		}
	}
	for i := 0; i < len(emitter.tag_directives); i++ {
		tag_directive := &emitter.tag_directives[i]
		if bytes.HasPrefix(tag, tag_directive.prefix) {
			emitter.tag_data.handle = tag_directive.handle
			emitter.tag_data.suffix = tag[len(tag_directive.prefix):]
			return nil
		}
	}
	emitter.tag_data.suffix = tag
	return nil
}

// Check if a scalar is valid.
func (emitter *Emitter) analyzeScalar(value []byte) error {
	var block_indicators,
		flow_indicators,
		line_breaks,
		special_characters,
		tab_characters,

		leading_space,
		leading_break,
		trailing_space,
		trailing_break,
		break_space,
		space_break,

		preceded_by_whitespace,
		followed_by_whitespace,
		previous_space,
		previous_break bool

	emitter.scalar_data.value = value

	if len(value) == 0 {
		emitter.scalar_data.multiline = false
		emitter.scalar_data.flow_plain_allowed = false
		emitter.scalar_data.block_plain_allowed = true
		emitter.scalar_data.single_quoted_allowed = true
		emitter.scalar_data.block_allowed = false
		return nil
	}

	if len(value) >= 3 && ((value[0] == '-' && value[1] == '-' && value[2] == '-') || (value[0] == '.' && value[1] == '.' && value[2] == '.')) {
		block_indicators = true
		flow_indicators = true
	}

	preceded_by_whitespace = true
	for i, w := 0, 0; i < len(value); i += w {
		w = width(value[i])
		followed_by_whitespace = i+w >= len(value) || isBlank(value, i+w)

		if i == 0 {
			switch value[i] {
			case '#', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
				flow_indicators = true
				block_indicators = true
			case '?', ':':
				flow_indicators = true
				if followed_by_whitespace {
					block_indicators = true
				}
			case '-':
				if followed_by_whitespace {
					flow_indicators = true
					block_indicators = true
				}
			}
		} else {
			switch value[i] {
			case ',', '?', '[', ']', '{', '}':
				flow_indicators = true
			case ':':
				flow_indicators = true
				if followed_by_whitespace {
					block_indicators = true
				}
			case '#':
				if preceded_by_whitespace {
					flow_indicators = true
					block_indicators = true
				}
			}
		}

		if value[i] == '\t' {
			tab_characters = true
		} else if !isPrintable(value, i) || !isASCII(value, i) && !emitter.unicode {
			special_characters = true
		}
		if isSpace(value, i) {
			if i == 0 {
				leading_space = true
			}
			if i+width(value[i]) == len(value) {
				trailing_space = true
			}
			if previous_break {
				break_space = true
			}
			previous_space = true
			previous_break = false
		} else if isLineBreak(value, i) {
			line_breaks = true
			if i == 0 {
				leading_break = true
			}
			if i+width(value[i]) == len(value) {
				trailing_break = true
			}
			if previous_space {
				space_break = true
			}
			previous_space = false
			previous_break = true
		} else {
			previous_space = false
			previous_break = false
		}

		// [Go]: Why 'z'? Couldn't be the end of the string as that's the loop condition.
		preceded_by_whitespace = isBlankOrZero(value, i)
	}

	emitter.scalar_data.multiline = line_breaks
	emitter.scalar_data.flow_plain_allowed = true
	emitter.scalar_data.block_plain_allowed = true
	emitter.scalar_data.single_quoted_allowed = true
	emitter.scalar_data.block_allowed = true

	if leading_space || leading_break || trailing_space || trailing_break {
		emitter.scalar_data.flow_plain_allowed = false
		emitter.scalar_data.block_plain_allowed = false
	}
	if trailing_space {
		emitter.scalar_data.block_allowed = false
	}
	if break_space {
		emitter.scalar_data.flow_plain_allowed = false
		emitter.scalar_data.block_plain_allowed = false
		emitter.scalar_data.single_quoted_allowed = false
	}
	if space_break || tab_characters || special_characters {
		emitter.scalar_data.flow_plain_allowed = false
		emitter.scalar_data.block_plain_allowed = false
		emitter.scalar_data.single_quoted_allowed = false
	}
	if space_break || special_characters {
		emitter.scalar_data.block_allowed = false
	}
	if line_breaks {
		emitter.scalar_data.flow_plain_allowed = false
		emitter.scalar_data.block_plain_allowed = false
	}
	if flow_indicators {
		emitter.scalar_data.flow_plain_allowed = false
	}
	if block_indicators {
		emitter.scalar_data.block_plain_allowed = false
	}
	return nil
}

// Check if the event data is valid.
func (emitter *Emitter) analyzeEvent(event *Event) error {
	emitter.anchor_data.anchor = nil
	emitter.tag_data.handle = nil
	emitter.tag_data.suffix = nil
	emitter.scalar_data.value = nil

	if len(event.HeadComment) > 0 {
		emitter.HeadComment = event.HeadComment
	}
	if len(event.LineComment) > 0 {
		emitter.LineComment = event.LineComment
	}
	if len(event.FootComment) > 0 {
		emitter.FootComment = event.FootComment
	}
	if len(event.TailComment) > 0 {
		emitter.TailComment = event.TailComment
	}

	switch event.Type {
	case ALIAS_EVENT:
		if err := emitter.analyzeAnchor(event.Anchor, true); err != nil {
			return err
		}

	case SCALAR_EVENT:
		if len(event.Anchor) > 0 {
			if err := emitter.analyzeAnchor(event.Anchor, false); err != nil {
				return err
			}
		}
		if len(event.Tag) > 0 && (emitter.canonical || (!event.Implicit && !event.quoted_implicit)) {
			if err := emitter.analyzeTag(event.Tag); err != nil {
				return err
			}
		}
		if err := emitter.analyzeScalar(event.Value); err != nil {
			return err
		}

	case SEQUENCE_START_EVENT:
		if len(event.Anchor) > 0 {
			if err := emitter.analyzeAnchor(event.Anchor, false); err != nil {
				return err
			}
		}
		if len(event.Tag) > 0 && (emitter.canonical || !event.Implicit) {
			if err := emitter.analyzeTag(event.Tag); err != nil {
				return err
			}
		}

	case MAPPING_START_EVENT:
		if len(event.Anchor) > 0 {
			if err := emitter.analyzeAnchor(event.Anchor, false); err != nil {
				return err
			}
		}
		if len(event.Tag) > 0 && (emitter.canonical || !event.Implicit) {
			if err := emitter.analyzeTag(event.Tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// Write the BOM character.
func (emitter *Emitter) writeBom() error {
	if err := emitter.flushIfNeeded(); err != nil {
		return err
	}
	pos := emitter.buffer_pos
	emitter.buffer[pos+0] = '\xEF'
	emitter.buffer[pos+1] = '\xBB'
	emitter.buffer[pos+2] = '\xBF'
	emitter.buffer_pos += 3
	return nil
}

func (emitter *Emitter) writeIndent() error {
	indent := emitter.indent
	if indent < 0 {
		indent = 0
	}
	if !emitter.indention || emitter.column > indent || (emitter.column == indent && !emitter.whitespace) {
		if err := emitter.putLineBreak(); err != nil {
			return err
		}
	}
	if emitter.foot_indent == indent {
		if err := emitter.putLineBreak(); err != nil {
			return err
		}
	}
	for emitter.column < indent {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}
	emitter.whitespace = true
	emitter.space_above = false
	emitter.foot_indent = -1
	return nil
}

func (emitter *Emitter) writeIndicator(indicator []byte, need_whitespace, is_whitespace, is_indention bool) error {
	if need_whitespace && !emitter.whitespace {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}
	if err := emitter.writeAll(indicator); err != nil {
		return err
	}
	emitter.whitespace = is_whitespace
	emitter.indention = (emitter.indention && is_indention)
	emitter.OpenEnded = false
	return nil
}

func (emitter *Emitter) writeAnchor(value []byte) error {
	if err := emitter.writeAll(value); err != nil {
		return err
	}
	emitter.whitespace = false
	emitter.indention = false
	return nil
}

func (emitter *Emitter) writeTagHandle(value []byte) error {
	if !emitter.whitespace {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}
	if err := emitter.writeAll(value); err != nil {
		return err
	}
	emitter.whitespace = false
	emitter.indention = false
	return nil
}

func (emitter *Emitter) writeTagContent(value []byte, need_whitespace bool) error {
	if need_whitespace && !emitter.whitespace {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}
	for i := 0; i < len(value); {
		var must_write bool
		switch value[i] {
		case ';', '/', '?', ':', '@', '&', '=', '+', '$', ',', '_', '.', '~', '*', '\'', '(', ')', '[', ']':
			must_write = true
		default:
			must_write = isAlpha(value, i)
		}
		if must_write {
			if err := emitter.write(value, &i); err != nil {
				return err
			}
		} else {
			w := width(value[i])
			for k := 0; k < w; k++ {
				octet := value[i]
				i++
				if err := emitter.put('%'); err != nil {
					return err
				}

				c := octet >> 4
				if c < 10 {
					c += '0'
				} else {
					c += 'A' - 10
				}
				if err := emitter.put(c); err != nil {
					return err
				}

				c = octet & 0x0f
				if c < 10 {
					c += '0'
				} else {
					c += 'A' - 10
				}
				if err := emitter.put(c); err != nil {
					return err
				}
			}
		}
	}
	emitter.whitespace = false
	emitter.indention = false
	return nil
}

func (emitter *Emitter) writePlainScalar(value []byte, allow_breaks bool) error {
	if len(value) > 0 && !emitter.whitespace {
		if err := emitter.put(' '); err != nil {
			return err
		}
	}

	spaces := false
	breaks := false
	for i := 0; i < len(value); {
		if isSpace(value, i) {
			if allow_breaks && !spaces && emitter.column > emitter.best_width && !isSpace(value, i+1) {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
				i += width(value[i])
			} else {
				if err := emitter.write(value, &i); err != nil {
					return err
				}
			}
			spaces = true
		} else if isLineBreak(value, i) {
			if !breaks && value[i] == '\n' {
				if err := emitter.putLineBreak(); err != nil {
					return err
				}
			}
			if err := emitter.writeLineBreak(value, &i); err != nil {
				return err
			}
			breaks = true
		} else {
			if breaks {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
			if err := emitter.write(value, &i); err != nil {
				return err
			}
			emitter.indention = false
			spaces = false
			breaks = false
		}
	}

	if len(value) > 0 {
		emitter.whitespace = false
	}
	emitter.indention = false
	if emitter.root_context {
		emitter.OpenEnded = true
	}

	return nil
}

func (emitter *Emitter) writeSingleQuotedScalar(value []byte, allow_breaks bool) error {
	if err := emitter.writeIndicator([]byte{'\''}, true, false, false); err != nil {
		return err
	}

	spaces := false
	breaks := false
	for i := 0; i < len(value); {
		if isSpace(value, i) {
			if allow_breaks && !spaces && emitter.column > emitter.best_width && i > 0 && i < len(value)-1 && !isSpace(value, i+1) {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
				i += width(value[i])
			} else {
				if err := emitter.write(value, &i); err != nil {
					return err
				}
			}
			spaces = true
		} else if isLineBreak(value, i) {
			if !breaks && value[i] == '\n' {
				if err := emitter.putLineBreak(); err != nil {
					return err
				}
			}
			if err := emitter.writeLineBreak(value, &i); err != nil {
				return err
			}
			breaks = true
		} else {
			if breaks {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
			if value[i] == '\'' {
				if err := emitter.put('\''); err != nil {
					return err
				}
			}
			if err := emitter.write(value, &i); err != nil {
				return err
			}
			emitter.indention = false
			spaces = false
			breaks = false
		}
	}
	if err := emitter.writeIndicator([]byte{'\''}, false, false, false); err != nil {
		return err
	}
	emitter.whitespace = false
	emitter.indention = false
	return nil
}

func (emitter *Emitter) writeDoubleQuotedScalar(value []byte, allow_breaks bool) error {
	spaces := false
	if err := emitter.writeIndicator([]byte{'"'}, true, false, false); err != nil {
		return err
	}

	for i := 0; i < len(value); {
		if !isPrintable(value, i) || (!emitter.unicode && !isASCII(value, i)) ||
			isBOM(value, i) || isLineBreak(value, i) ||
			value[i] == '"' || value[i] == '\\' {

			octet := value[i]

			var w int
			var v rune
			switch {
			case octet&0x80 == 0x00:
				w, v = 1, rune(octet&0x7F)
			case octet&0xE0 == 0xC0:
				w, v = 2, rune(octet&0x1F)
			case octet&0xF0 == 0xE0:
				w, v = 3, rune(octet&0x0F)
			case octet&0xF8 == 0xF0:
				w, v = 4, rune(octet&0x07)
			}
			for k := 1; k < w; k++ {
				octet = value[i+k]
				v = (v << 6) + (rune(octet) & 0x3F)
			}
			i += w

			if err := emitter.put('\\'); err != nil {
				return err
			}

			var err error
			switch v {
			case 0x00:
				err = emitter.put('0')
			case 0x07:
				err = emitter.put('a')
			case 0x08:
				err = emitter.put('b')
			case 0x09:
				err = emitter.put('t')
			case 0x0A:
				err = emitter.put('n')
			case 0x0b:
				err = emitter.put('v')
			case 0x0c:
				err = emitter.put('f')
			case 0x0d:
				err = emitter.put('r')
			case 0x1b:
				err = emitter.put('e')
			case 0x22:
				err = emitter.put('"')
			case 0x5c:
				err = emitter.put('\\')
			case 0x85:
				err = emitter.put('N')
			case 0xA0:
				err = emitter.put('_')
			case 0x2028:
				err = emitter.put('L')
			case 0x2029:
				err = emitter.put('P')
			default:
				if v <= 0xFF {
					err = emitter.put('x')
					w = 2
				} else if v <= 0xFFFF {
					err = emitter.put('u')
					w = 4
				} else {
					err = emitter.put('U')
					w = 8
				}
				for k := (w - 1) * 4; err == nil && k >= 0; k -= 4 {
					digit := byte((v >> uint(k)) & 0x0F)
					if digit < 10 {
						err = emitter.put(digit + '0')
					} else {
						err = emitter.put(digit + 'A' - 10)
					}
				}
			}
			if err != nil {
				return err
			}
			spaces = false
		} else if isSpace(value, i) {
			if allow_breaks && !spaces && emitter.column > emitter.best_width && i > 0 && i < len(value)-1 {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
				if isSpace(value, i+1) {
					if err := emitter.put('\\'); err != nil {
						return err
					}
				}
				i += width(value[i])
			} else if err := emitter.write(value, &i); err != nil {
				return err
			}
			spaces = true
		} else {
			if err := emitter.write(value, &i); err != nil {
				return err
			}
			spaces = false
		}
	}
	if err := emitter.writeIndicator([]byte{'"'}, false, false, false); err != nil {
		return err
	}
	emitter.whitespace = false
	emitter.indention = false
	return nil
}

func (emitter *Emitter) writeBlockScalarHints(value []byte) error {
	if isSpace(value, 0) {
		// https://github.com/yaml/go-yaml/issues/65
		// isLineBreak(value, 0) removed as the linebreak will only write the indentation value.
		indent_hint := []byte{'0' + byte(emitter.BestIndent)}
		if err := emitter.writeIndicator(indent_hint, false, false, false); err != nil {
			return err
		}
	}

	emitter.OpenEnded = false

	var chomp_hint [1]byte
	if len(value) == 0 {
		chomp_hint[0] = '-'
	} else {
		i := len(value) - 1
		for value[i]&0xC0 == 0x80 {
			i--
		}
		if !isLineBreak(value, i) {
			chomp_hint[0] = '-'
		} else if i == 0 {
			chomp_hint[0] = '+'
			emitter.OpenEnded = true
		} else {
			i--
			for value[i]&0xC0 == 0x80 {
				i--
			}
			if isLineBreak(value, i) {
				chomp_hint[0] = '+'
				emitter.OpenEnded = true
			}
		}
	}
	if chomp_hint[0] != 0 {
		if err := emitter.writeIndicator(chomp_hint[:], false, false, false); err != nil {
			return err
		}
	}
	return nil
}

func (emitter *Emitter) writeLiteralScalar(value []byte) error {
	if err := emitter.writeIndicator([]byte{'|'}, true, false, false); err != nil {
		return err
	}
	if err := emitter.writeBlockScalarHints(value); err != nil {
		return err
	}
	if err := emitter.processLineCommentLinebreak(true); err != nil {
		return err
	}
	emitter.whitespace = true
	breaks := true
	for i := 0; i < len(value); {
		if isLineBreak(value, i) {
			if err := emitter.writeLineBreak(value, &i); err != nil {
				return err
			}
			breaks = true
		} else {
			if breaks {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
			if err := emitter.write(value, &i); err != nil {
				return err
			}
			emitter.indention = false
			breaks = false
		}
	}

	return nil
}

func (emitter *Emitter) writeFoldedScalar(value []byte) error {
	if err := emitter.writeIndicator([]byte{'>'}, true, false, false); err != nil {
		return err
	}
	if err := emitter.writeBlockScalarHints(value); err != nil {
		return err
	}
	if err := emitter.processLineCommentLinebreak(true); err != nil {
		return err
	}

	emitter.whitespace = true

	breaks := true
	leading_spaces := true
	for i := 0; i < len(value); {
		if isLineBreak(value, i) {
			if !breaks && !leading_spaces && value[i] == '\n' {
				k := 0
				for isLineBreak(value, k) {
					k += width(value[k])
				}
				if !isBlankOrZero(value, k) {
					if err := emitter.putLineBreak(); err != nil {
						return err
					}
				}
			}
			if err := emitter.writeLineBreak(value, &i); err != nil {
				return err
			}
			breaks = true
		} else {
			if breaks {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
				leading_spaces = isBlank(value, i)
			}
			if !breaks && isSpace(value, i) && !isSpace(value, i+1) && emitter.column > emitter.best_width {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
				i += width(value[i])
			} else {
				if err := emitter.write(value, &i); err != nil {
					return err
				}
			}
			emitter.indention = false
			breaks = false
		}
	}
	return nil
}

func (emitter *Emitter) writeComment(comment []byte) error {
	breaks := false
	pound := false
	for i := 0; i < len(comment); {
		if isLineBreak(comment, i) {
			if err := emitter.writeLineBreak(comment, &i); err != nil {
				return err
			}
			breaks = true
			pound = false
		} else {
			if breaks {
				if err := emitter.writeIndent(); err != nil {
					return err
				}
			}
			if !pound {
				if comment[i] != '#' {
					if err := emitter.put('#'); err != nil {
						return err
					}
					if err := emitter.put(' '); err != nil {
						return err
					}
				}
				pound = true
			}
			if err := emitter.write(comment, &i); err != nil {
				return err
			}
			emitter.indention = false
			breaks = false
		}
	}
	if !breaks {
		if err := emitter.putLineBreak(); err != nil {
			return err
		}
	}

	emitter.whitespace = true
	return nil
}
