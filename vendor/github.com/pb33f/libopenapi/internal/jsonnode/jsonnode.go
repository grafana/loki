// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package jsonnode builds the yaml.Node tree for a JSON document directly, without running yaml's scanner.
//
// yaml parses JSON as a flow collection, and its scanner treats the opening '{' as a potential simple key,
// so it buffers every token of the document until the closing '}'. For a large JSON specification that is
// hundreds of megabytes of token queue. JSON's grammar is small enough to parse directly into the exact
// tree yaml would produce: the same kinds, tags, styles, values, lines and columns.
//
// Parse only accepts input whose yaml parse it reproduces exactly. Anything else (invalid JSON, or valid
// JSON that yaml reads differently, such as a key separated from its colon by a line break) is declined,
// and the caller parses it with yaml instead, getting yaml's result or yaml's error.
package jsonnode

import (
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/pb33f/go-yaml"
)

const (
	// maxDepth bounds nesting well inside yaml's own depth limit.
	maxDepth = 1000

	// internLimit is the longest string interned. Keys and short values repeat heavily across a
	// specification; long text rarely does and is not worth hashing.
	internLimit = 64

	nodeSlabSize    = 512
	contentSlabSize = 2048
)

// yamlStyleFloat is the pattern yaml's resolver requires of a plain scalar before resolving it as a float.
var yamlStyleFloat = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)

type parser struct {
	data []byte
	pos  int
	line int // 1-based line of data[pos]
	col  int // 1-based column of data[pos], counted in characters as yaml counts them

	depth   int
	nodes   []yaml.Node  // slab the tree's nodes are allocated from
	content []*yaml.Node // slab the collections' Content slices are carved from
	stack   []*yaml.Node // children of the collections still open
	strs    map[string]string
	buf     []byte // scratch space for decoding escaped strings
}

// Unmarshal decodes data into out, producing exactly what yaml.Unmarshal produces: a JSON object is built
// by Parse, and anything Parse declines is decoded by yaml.Unmarshal.
func Unmarshal(data []byte, out *yaml.Node) error {
	if doc, ok := Parse(data); ok {
		*out = *doc
		return nil
	}
	return yaml.Unmarshal(data, out)
}

// Parse returns the yaml document node yaml.Unmarshal produces for the JSON document in data, and true.
// It returns false when data is not a JSON object whose yaml parse it reproduces exactly.
func Parse(data []byte) (*yaml.Node, bool) {
	p := parser{data: data, line: 1, col: 1}
	if !p.skipSpace(false) || p.pos >= len(data) || data[p.pos] != '{' {
		return nil, false
	}
	p.strs = make(map[string]string, 256)
	doc := &yaml.Node{Kind: yaml.DocumentNode, Line: p.line, Column: p.col}
	root, ok := p.parseValue()
	if !ok || !p.skipSpace(false) || p.pos != len(data) {
		return nil, false
	}
	doc.Content = []*yaml.Node{root}
	return doc, true
}

func (p *parser) newNode(kind yaml.Kind, style yaml.Style, tag string) *yaml.Node {
	if len(p.nodes) == cap(p.nodes) {
		p.nodes = make([]yaml.Node, 0, nodeSlabSize)
	}
	p.nodes = append(p.nodes, yaml.Node{Kind: kind, Style: style, Tag: tag, Line: p.line, Column: p.col})
	return &p.nodes[len(p.nodes)-1]
}

// carve returns a copy of children in a slice from the content slab. Its capacity equals its length, so
// appending to a node's Content never writes into a neighbour's.
func (p *parser) carve(children []*yaml.Node) []*yaml.Node {
	n := len(children)
	if cap(p.content)-len(p.content) < n {
		p.content = make([]*yaml.Node, 0, max(contentSlabSize, n))
	}
	start := len(p.content)
	p.content = append(p.content, children...)
	return p.content[start : start+n : start+n]
}

func (p *parser) intern(b []byte) string {
	if len(b) > internLimit {
		return string(b)
	}
	if s, ok := p.strs[string(b)]; ok {
		return s
	}
	s := string(b)
	p.strs[s] = s
	return s
}

// skipSpace consumes JSON whitespace. Outside any collection yaml does not accept a tab, so one declines.
func (p *parser) skipSpace(inCollection bool) bool {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ':
			p.pos++
			p.col++
		case '\t':
			if !inCollection {
				return false
			}
			p.pos++
			p.col++
		case '\n':
			p.pos++
			p.line++
			p.col = 1
		case '\r':
			// yaml reads CR LF as one line break; a lone CR is declined.
			if p.pos+1 >= len(p.data) || p.data[p.pos+1] != '\n' {
				return false
			}
			p.pos += 2
			p.line++
			p.col = 1
		default:
			return true
		}
	}
	return true
}

func (p *parser) parseValue() (*yaml.Node, bool) {
	if p.pos >= len(p.data) {
		return nil, false
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.parseCollection(yaml.MappingNode, "!!map", '}')
	case c == '[':
		return p.parseCollection(yaml.SequenceNode, "!!seq", ']')
	case c == '"':
		return p.parseString()
	case c == 't':
		return p.parseLiteral("true", "!!bool")
	case c == 'f':
		return p.parseLiteral("false", "!!bool")
	case c == 'n':
		return p.parseLiteral("null", "!!null")
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	}
	return nil, false
}

func (p *parser) parseCollection(kind yaml.Kind, tag string, closer byte) (*yaml.Node, bool) {
	p.depth++
	if p.depth > maxDepth {
		return nil, false
	}
	node := p.newNode(kind, yaml.FlowStyle, tag)
	p.pos++
	p.col++
	base := len(p.stack)
	if !p.skipSpace(true) {
		return nil, false
	}
	if p.pos < len(p.data) && p.data[p.pos] == closer {
		p.pos++
		p.col++
		p.depth--
		return node, true
	}
	for {
		if kind == yaml.MappingNode {
			if p.pos >= len(p.data) || p.data[p.pos] != '"' {
				return nil, false
			}
			keyLine := p.line
			key, ok := p.parseString()
			if !ok || !p.skipSpace(true) {
				return nil, false
			}
			// yaml only pairs a key with a colon on the same line.
			if p.pos >= len(p.data) || p.data[p.pos] != ':' || p.line != keyLine {
				return nil, false
			}
			p.pos++
			p.col++
			if !p.skipSpace(true) {
				return nil, false
			}
			p.stack = append(p.stack, key)
		}
		value, ok := p.parseValue()
		if !ok || !p.skipSpace(true) || p.pos >= len(p.data) {
			return nil, false
		}
		p.stack = append(p.stack, value)
		switch p.data[p.pos] {
		case ',':
			p.pos++
			p.col++
			if !p.skipSpace(true) {
				return nil, false
			}
			continue
		case closer:
			p.pos++
			p.col++
			node.Content = p.carve(p.stack[base:])
			clear(p.stack[base:])
			p.stack = p.stack[:base]
			p.depth--
			return node, true
		}
		return nil, false
	}
}

func (p *parser) parseLiteral(word, tag string) (*yaml.Node, bool) {
	if len(p.data)-p.pos < len(word) || string(p.data[p.pos:p.pos+len(word)]) != word {
		return nil, false
	}
	node := p.newNode(yaml.ScalarNode, 0, tag)
	node.Value = word
	p.pos += len(word)
	p.col += len(word)
	return node, true
}

func (p *parser) parseNumber() (*yaml.Node, bool) {
	start := p.pos
	i := p.pos
	if p.data[i] == '-' {
		i++
	}
	switch {
	case i < len(p.data) && p.data[i] == '0':
		i++
	case i < len(p.data) && p.data[i] >= '1' && p.data[i] <= '9':
		i = skipDigits(p.data, i)
	default:
		return nil, false
	}
	if i < len(p.data) && p.data[i] == '.' {
		i++
		if i >= len(p.data) || p.data[i] < '0' || p.data[i] > '9' {
			return nil, false
		}
		i = skipDigits(p.data, i)
	}
	if i < len(p.data) && (p.data[i] == 'e' || p.data[i] == 'E') {
		i++
		if i < len(p.data) && (p.data[i] == '+' || p.data[i] == '-') {
			i++
		}
		if i >= len(p.data) || p.data[i] < '0' || p.data[i] > '9' {
			return nil, false
		}
		i = skipDigits(p.data, i)
	}
	value := p.intern(p.data[start:i])
	node := p.newNode(yaml.ScalarNode, 0, numberTag(value))
	node.Value = value
	p.col += i - start
	p.pos = i
	return node, true
}

func skipDigits(data []byte, i int) int {
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		i++
	}
	return i
}

// numberTag resolves a JSON number the way yaml's resolver resolves a plain scalar starting with a digit
// or sign: negative zero is a float, then an integer when it parses as a signed or unsigned 64-bit
// integer, a float when it parses as one, and a string otherwise (a float literal out of range).
func numberTag(value string) string {
	if value == "-0" || value == "-0.0" {
		return "!!float"
	}
	if _, err := strconv.ParseInt(value, 0, 64); err == nil {
		return "!!int"
	}
	if _, err := strconv.ParseUint(value, 0, 64); err == nil {
		return "!!int"
	}
	if yamlStyleFloat.MatchString(value) {
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return "!!float"
		}
	}
	return "!!str"
}

func (p *parser) parseString() (*yaml.Node, bool) {
	node := p.newNode(yaml.ScalarNode, yaml.DoubleQuotedStyle, "!!str")
	p.pos++
	p.col++
	start := p.pos
	escaped := false
	p.buf = p.buf[:0]
	for {
		if p.pos >= len(p.data) {
			return nil, false
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			if escaped {
				node.Value = p.intern(p.buf)
			} else {
				node.Value = p.intern(p.data[start:p.pos])
			}
			p.pos++
			p.col++
			return node, true
		case c == '\\':
			if !escaped {
				escaped = true
				p.buf = append(p.buf, p.data[start:p.pos]...)
			}
			if !p.decodeEscape() {
				return nil, false
			}
		case c >= 0x20 && c < 0x7F:
			if escaped {
				p.buf = append(p.buf, c)
			}
			p.pos++
			p.col++
		case c < 0x80:
			// control characters and DEL: JSON forbids the former and yaml reads neither like JSON does.
			return nil, false
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if !plainRune(r, size) {
				return nil, false
			}
			if escaped {
				p.buf = append(p.buf, p.data[p.pos:p.pos+size]...)
			}
			p.pos += size
			p.col++
		}
	}
}

// plainRune reports whether yaml reads a non-ASCII character inside a quoted scalar as itself: valid
// UTF-8 in the character set yaml accepts, and not one of the characters it treats as a line break or
// byte order mark, or as a C1 control.
func plainRune(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		return false
	}
	switch {
	case r <= 0x9F: // C1 controls, including NEL
		return false
	case r == 0x2028 || r == 0x2029 || r == 0xFEFF:
		return false
	case r >= 0xFFFE && r <= 0xFFFF:
		return false
	}
	return true
}

// decodeEscape decodes the JSON escape at p.pos into p.buf. yaml has no "\\/" escape, so that one is declined.
func (p *parser) decodeEscape() bool {
	if p.pos+1 >= len(p.data) {
		return false
	}
	var b byte
	switch p.data[p.pos+1] {
	case '"':
		b = '"'
	case '\\':
		b = '\\'
	case 'b':
		b = '\b'
	case 'f':
		b = '\f'
	case 'n':
		b = '\n'
	case 'r':
		b = '\r'
	case 't':
		b = '\t'
	case 'u':
		if p.pos+6 > len(p.data) {
			return false
		}
		code := rune(0)
		for _, h := range p.data[p.pos+2 : p.pos+6] {
			d, ok := hexValue(h)
			if !ok {
				return false
			}
			code = code<<4 | d
		}
		// yaml rejects an escaped surrogate; the caller's yaml parse reports it.
		if code >= 0xD800 && code <= 0xDFFF {
			return false
		}
		p.buf = utf8.AppendRune(p.buf, code)
		p.pos += 6
		p.col += 6
		return true
	default:
		return false
	}
	p.buf = append(p.buf, b)
	p.pos += 2
	p.col += 2
	return true
}

func hexValue(h byte) (rune, bool) {
	switch {
	case h >= '0' && h <= '9':
		return rune(h - '0'), true
	case h >= 'a' && h <= 'f':
		return rune(h-'a') + 10, true
	case h >= 'A' && h <= 'F':
		return rune(h-'A') + 10, true
	}
	return 0, false
}
