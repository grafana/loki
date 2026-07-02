// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Composer stage: Builds a node tree from a libyaml event stream.
// Handles document structure, anchors, and comment attachment.

package libyaml

import (
	"fmt"
	"io"
)

// Composer produces a node tree out of a libyaml event stream.
type Composer struct {
	Parser       Parser
	event        Event
	doc          *Node
	anchors      map[string]*Node
	doneInit     bool
	Textless     bool
	streamNodes  bool     // enable stream node emission
	returnStream bool     // flag to return stream node next
	atStreamEnd  bool     // at stream end
	encoding     Encoding // stream encoding from STREAM_START
	opts         *Options // options for loading
}

// NewComposer creates a new composer from a byte slice.
func NewComposer(b []byte, opts *Options) *Composer {
	p := Composer{
		Parser: NewParser(),
		opts:   opts,
	}
	if len(b) == 0 {
		b = []byte{'\n'}
	}
	p.Parser.SetInputString(b)
	if opts != nil {
		p.Parser.depthCheck = opts.DepthCheck
	}
	return &p
}

// NewComposerFromReader creates a new composer from an [io.Reader].
func NewComposerFromReader(r io.Reader, opts *Options) *Composer {
	p := Composer{
		Parser: NewParser(),
		opts:   opts,
	}
	p.Parser.SetInputReader(r)
	if opts != nil {
		p.Parser.depthCheck = opts.DepthCheck
	}
	return &p
}

// Compose composes the next YAML node from the event stream.
func (c *Composer) Compose() *Node {
	c.init()

	// Handle stream nodes if enabled
	if c.streamNodes {
		// Check for stream end first
		if c.peek() == STREAM_END_EVENT {
			// If we haven't returned the final stream node yet,
			// return it now
			if !c.atStreamEnd {
				c.atStreamEnd = true
				return c.createStreamNode()
			}
			// Already returned final stream node
			return nil
		}

		// Check if we should return a stream node before the next
		// document
		if c.returnStream {
			c.returnStream = false
			n := c.createStreamNode()
			// Capture directives from upcoming document
			c.captureDirectives(n)
			return n
		}
	}

	switch c.peek() {
	case SCALAR_EVENT:
		return c.scalar()
	case ALIAS_EVENT:
		return c.alias()
	case MAPPING_START_EVENT:
		return c.mapping()
	case SEQUENCE_START_EVENT:
		return c.sequence()
	case DOCUMENT_START_EVENT:
		return c.document()
	case STREAM_END_EVENT:
		// Happens when attempting to decode an empty buffer (when not
		// using stream nodes).
		return nil
	case TAIL_COMMENT_EVENT:
		panic("internal error: unexpected tail comment event (please report)")
	default:
		panic("internal error: attempted to parse unknown event (please report): " + c.event.Type.String())
	}
}

// node creates a new node with the given kind, tag, and value, and attaches
// position and comment information from the current event.
func (c *Composer) node(kind Kind, tag, value string) *Node {
	var style Style
	if tag != "" && tag != "!" {
		// Normalize tag to short form (e.g., tag:yaml.org,2002:str -> !!str)
		tag = shortTag(tag)
		style = TaggedStyle
	}
	// Note: Nodes without explicit tags are left with empty tags.
	// Tag defaulting happens in a separate stage via Resolver.
	n := &Node{
		Kind:  kind,
		Tag:   tag,
		Value: value,
		Style: style,
	}
	if !c.Textless {
		n.Line = c.event.StartMark.Line
		n.Column = c.event.StartMark.Column
		n.HeadComment = string(c.event.HeadComment)
		n.LineComment = string(c.event.LineComment)
		n.FootComment = string(c.event.FootComment)
	}
	return n
}

// document composes a document node by parsing its content between
// DOCUMENT_START and DOCUMENT_END events.
func (c *Composer) document() *Node {
	n := c.node(DocumentNode, "", "")
	c.doc = n
	c.expect(DOCUMENT_START_EVENT)
	c.parseChild(n)
	if c.peek() == DOCUMENT_END_EVENT {
		n.FootComment = string(c.event.FootComment)
	}
	c.expect(DOCUMENT_END_EVENT)

	// If stream nodes enabled, prepare to return a stream node next
	if c.streamNodes {
		c.returnStream = true
	}

	return n
}

// createStreamNode creates a stream node with encoding information.
func (c *Composer) createStreamNode() *Node {
	n := &Node{
		Kind:   StreamNode,
		Stream: &Stream{Encoding: c.encoding},
	}
	if !c.Textless && c.event.Type != NO_EVENT {
		n.Line = c.event.StartMark.Line
		n.Column = c.event.StartMark.Column
	}
	return n
}

// alias composes an alias node by resolving the referenced anchor.
func (c *Composer) alias() *Node {
	n := c.node(AliasNode, "", string(c.event.Anchor))
	n.Alias = c.anchors[n.Value]
	if n.Alias == nil {
		msg := fmt.Sprintf("unknown anchor '%s' referenced", n.Value)
		Fail(formatComposerError(msg, Mark{
			Line:   n.Line,
			Column: n.Column,
		}))
	}
	c.expect(ALIAS_EVENT)
	return n
}

// scalar composes a scalar node with value, tag, and style information.
func (c *Composer) scalar() *Node {
	parsedStyle := c.event.ScalarStyle()
	var nodeStyle Style
	switch {
	case parsedStyle&DOUBLE_QUOTED_SCALAR_STYLE != 0:
		nodeStyle = DoubleQuotedStyle
	case parsedStyle&SINGLE_QUOTED_SCALAR_STYLE != 0:
		nodeStyle = SingleQuotedStyle
	case parsedStyle&LITERAL_SCALAR_STYLE != 0:
		nodeStyle = LiteralStyle
	case parsedStyle&FOLDED_SCALAR_STYLE != 0:
		nodeStyle = FoldedStyle
	}
	nodeValue := string(c.event.Value)
	nodeTag := string(c.event.Tag)
	n := c.node(ScalarNode, nodeTag, nodeValue)
	n.Style |= nodeStyle
	c.anchor(n, c.event.Anchor)
	c.expect(SCALAR_EVENT)
	return n
}

// sequence composes a sequence node by parsing elements between
// SEQUENCE_START and SEQUENCE_END events.
func (c *Composer) sequence() *Node {
	n := c.node(SequenceNode, string(c.event.Tag), "")
	if c.event.SequenceStyle()&FLOW_SEQUENCE_STYLE != 0 {
		n.Style |= FlowStyle
	}
	c.anchor(n, c.event.Anchor)
	c.expect(SEQUENCE_START_EVENT)
	for c.peek() != SEQUENCE_END_EVENT {
		c.parseChild(n)
	}
	n.LineComment = string(c.event.LineComment)
	n.FootComment = string(c.event.FootComment)
	c.expect(SEQUENCE_END_EVENT)
	return n
}

// mapping composes a mapping node by parsing key-value pairs between
// MAPPING_START and MAPPING_END events, handling foot comments appropriately.
func (c *Composer) mapping() *Node {
	n := c.node(MappingNode, string(c.event.Tag), "")
	block := true
	if c.event.MappingStyle()&FLOW_MAPPING_STYLE != 0 {
		block = false
		n.Style |= FlowStyle
	}
	c.anchor(n, c.event.Anchor)
	c.expect(MAPPING_START_EVENT)
	for c.peek() != MAPPING_END_EVENT {
		k := c.parseChild(n)
		if block && k.FootComment != "" {
			// Must be a foot comment for the prior value when being dedented.
			if len(n.Content) > 2 {
				n.Content[len(n.Content)-3].FootComment = k.FootComment
				k.FootComment = ""
			}
		}
		v := c.parseChild(n)
		if k.FootComment == "" && v.FootComment != "" {
			k.FootComment = v.FootComment
			v.FootComment = ""
		}
		if c.peek() == TAIL_COMMENT_EVENT {
			if k.FootComment == "" {
				k.FootComment = string(c.event.FootComment)
			}
			c.expect(TAIL_COMMENT_EVENT)
		}
	}
	n.LineComment = string(c.event.LineComment)
	n.FootComment = string(c.event.FootComment)
	if n.Style&FlowStyle == 0 && n.FootComment != "" && len(n.Content) > 1 {
		n.Content[len(n.Content)-2].FootComment = n.FootComment
		n.FootComment = ""
	}
	c.expect(MAPPING_END_EVENT)
	return n
}

// init initializes the composer by setting up the anchor map and consuming
// the STREAM_START event.
func (c *Composer) init() {
	if c.doneInit {
		return
	}
	c.anchors = make(map[string]*Node)
	// Peek to get the encoding from STREAM_START_EVENT
	if c.peek() == STREAM_START_EVENT {
		c.encoding = c.event.GetEncoding()
	}
	c.expect(STREAM_START_EVENT)
	c.doneInit = true

	// If stream nodes are enabled, prepare to return the first stream node
	if c.streamNodes {
		c.returnStream = true
	}
}

// Destroy cleans up the composer by deleting any pending event and the
// underlying parser.
func (c *Composer) Destroy() {
	if c.event.Type != NO_EVENT {
		c.event.Delete()
	}
	c.Parser.Delete()
}

// SetStreamNodes enables or disables stream node emission.
func (c *Composer) SetStreamNodes(enable bool) {
	c.streamNodes = enable
}

// expect consumes an event from the event stream and
// checks that it's of the expected type.
func (c *Composer) expect(e EventType) {
	if c.event.Type == NO_EVENT {
		if err := c.Parser.Parse(&c.event); err != nil {
			c.fail(err)
		}
	}
	if c.event.Type == STREAM_END_EVENT {
		Fail(formatComposerError(
			"attempted to go past the end of stream; corrupted value?",
			Mark{Line: c.event.StartMark.Line, Column: c.event.StartMark.Column},
		))
	}
	if c.event.Type != e {
		Fail(formatComposerError(
			fmt.Sprintf("expected %s event but got %s", e, c.event.Type),
			Mark{Line: c.event.StartMark.Line, Column: c.event.StartMark.Column},
		))
	}
	c.event.Delete()
	c.event.Type = NO_EVENT
}

// peek peeks at the next event in the event stream,
// puts the results into c.event and returns the event type.
func (c *Composer) peek() EventType {
	if c.event.Type != NO_EVENT {
		return c.event.Type
	}
	// It's curious choice from the underlying API to generally return a
	// positive result on success, but on this case return true in an error
	// scenario. This was the source of bugs in the past (issue #666).
	if err := c.Parser.Parse(&c.event); err != nil {
		c.fail(err)
	}
	return c.event.Type
}

// fail panics with the given error.
func (c *Composer) fail(err error) {
	Fail(err)
}

// anchor sets the anchor name on a node and records it in the anchor map.
func (c *Composer) anchor(n *Node, anchor []byte) {
	if anchor != nil {
		n.Anchor = string(anchor)
		c.anchors[n.Anchor] = n
	}
}

// parseChild composes the next node and adds it as a child to the parent.
func (c *Composer) parseChild(parent *Node) *Node {
	child := c.Compose()
	parent.Content = append(parent.Content, child)
	return child
}

// captureDirectives captures version and tag directives from upcoming
// DOCUMENT_START.
// The node n must have Stream initialized (as created by createStreamNode).
func (c *Composer) captureDirectives(n *Node) {
	if c.peek() == DOCUMENT_START_EVENT {
		if vd := c.event.GetVersionDirective(); vd != nil {
			n.Stream.Version = &StreamVersionDirective{
				Major: vd.Major(),
				Minor: vd.Minor(),
			}
		}
		if tds := c.event.GetTagDirectives(); len(tds) > 0 {
			n.Stream.TagDirectives = make([]StreamTagDirective, len(tds))
			for i, td := range tds {
				n.Stream.TagDirectives[i] = StreamTagDirective{
					Handle: td.GetHandle(),
					Prefix: td.GetPrefix(),
				}
			}
		}
	}
}

// Fail panics with a YAMLError wrapping the given error.
func Fail(err error) {
	panic(&YAMLError{err})
}

// failf panics with a YAMLError containing a formatted error message.
func failf(format string, args ...any) {
	panic(&YAMLError{fmt.Errorf("yaml: "+format, args...)})
}

// formatComposerError creates a LoadError for composer-stage errors.
func formatComposerError(message string, mark Mark) *LoadError {
	return &LoadError{
		Stage:   ComposerStage,
		Mark:    mark,
		Message: message,
	}
}

// formatComposerErrorContext creates a LoadError with both context and
// problem information for composer-stage errors.
func formatComposerErrorContext(context string, contextMark Mark, message string, mark Mark) *LoadError {
	return &LoadError{
		Stage:       ComposerStage,
		ContextMark: contextMark,
		ContextMsg:  context,
		Mark:        mark,
		Message:     message,
	}
}
