package jsonlite

import (
	"fmt"
	"unsafe"
)

// This file implements "stage 2": a recursive-descent parser that consumes
// the structural index produced by structuralIndex instead of re-scanning
// input bytes. It produces the same Value trees as parseValue.

// indexCursor walks the structural index of a document.
type indexCursor struct {
	s     string
	index []uint32
	pos   int
	hasBS bool // document contains backslashes: string escapes need validation
}

// parseIndexed is the structural-index equivalent of ParseMaxDepth.
func parseIndexed(data string, maxDepth int) (*Value, error) {
	indexPtr := indexPool.Get().(*[]uint32)
	index, flags, err := structuralIndex(data, (*indexPtr)[:0])
	*indexPtr = index
	if err != nil {
		indexPool.Put(indexPtr)
		return nil, err
	}
	c := indexCursor{s: data, index: index, hasBS: flags&flagBackslash != 0}
	// As in ParseMaxDepth, container parsing acquires the pooled scratch on
	// first use.
	v, err := parseIndexedValue(&c, max(0, maxDepth), nil)
	if err == nil && c.pos != len(c.index) {
		err = fmt.Errorf("unexpected token after root value at offset %d", c.index[c.pos])
	}
	indexPool.Put(indexPtr)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// stringToken consumes a string token starting at the opening quote.
// The next emitted index after an opening quote is always its closing quote.
func (c *indexCursor) stringToken() (string, error) {
	i := c.index[c.pos]
	if c.pos+1 >= len(c.index) {
		return "", errUnterminatedString
	}
	j := c.index[c.pos+1]
	if c.s[j] != '"' {
		return "", fmt.Errorf("malformed string at offset %d", i)
	}
	c.pos += 2
	return c.s[i : j+1], nil
}

// primitiveToken consumes a number/literal token starting at position i.
// The token extends to the next emitted index (or end of input), minus
// trailing whitespace.
func (c *indexCursor) primitiveToken() string {
	i := c.index[c.pos]
	c.pos++
	end := len(c.s)
	if c.pos < len(c.index) {
		end = int(c.index[c.pos])
	}
	for end > int(i) && isWhitespace(c.s[end-1]) {
		end--
	}
	return c.s[i:end]
}

// validIndexedString validates escape sequences in a string token. When the
// document contains no backslashes, stage 1 has already fully validated the
// string (terminated, no raw control characters), so no scan is needed.
func (c *indexCursor) validIndexedString(token string) bool {
	if !c.hasBS {
		return true
	}
	return validString(token)
}

func parseIndexedValue(c *indexCursor, maxDepth int, p *parser) (Value, error) {
	if c.pos >= len(c.index) {
		return Value{}, errUnexpectedEndOfObject
	}
	i := c.index[c.pos]
	switch c.s[i] {
	case '{':
		// The cached JSON substring starts right after the previous token,
		// including any leading whitespace, to match parseValue's behavior.
		start := 0
		if c.pos > 0 {
			start = int(c.index[c.pos-1]) + 1
		}
		c.pos++
		return parseIndexedObject(c, start, maxDepth, p)
	case '[':
		start := 0
		if c.pos > 0 {
			start = int(c.index[c.pos-1]) + 1
		}
		c.pos++
		return parseIndexedArray(c, start, maxDepth, p)
	case '"':
		token, err := c.stringToken()
		if err != nil {
			return Value{}, err
		}
		if !c.validIndexedString(token) {
			return Value{}, fmt.Errorf("invalid token: %q", token)
		}
		return makeStringValue(token), nil
	case ']':
		c.pos++
		return Value{}, errEndOfArray
	case '}':
		c.pos++
		return Value{}, errEndOfObject
	case ',', ':':
		c.pos++
		return Value{}, fmt.Errorf("unexpected token %q", c.s[i:i+1])
	default:
		token := c.primitiveToken()
		switch token[0] {
		case 'n':
			if token != "null" {
				return Value{}, fmt.Errorf("invalid token: %q", token)
			}
			return makeNullValue(token), nil
		case 't':
			if token != "true" {
				return Value{}, fmt.Errorf("invalid token: %q", token)
			}
			return makeTrueValue(token), nil
		case 'f':
			if token != "false" {
				return Value{}, fmt.Errorf("invalid token: %q", token)
			}
			return makeFalseValue(token), nil
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if !validNumber(token) {
				return Value{}, fmt.Errorf("invalid number: %q", token)
			}
			return makeNumberValue(token), nil
		default:
			return Value{}, fmt.Errorf("invalid token: %q", token)
		}
	}
}

func parseIndexedArray(c *indexCursor, start, maxDepth int, p *parser) (Value, error) {
	if p == nil {
		p = getParser()
		defer putParser(p)
	}
	base := len(p.values)

	for i := 0; ; i++ {
		if i != 0 {
			if c.pos >= len(c.index) {
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return Value{}, errUnexpectedEndOfArray
			}
			j := c.index[c.pos]
			if c.s[j] == ']' {
				c.pos++
				cached := c.s[start : j+1]
				result := make([]Value, len(p.values)-base+1)
				result[0] = makeStringValue(cached)
				copy(result[1:], p.values[base:])
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return makeArrayValue(result), nil
			}
			if c.s[j] != ',' {
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return Value{}, fmt.Errorf("expected ',' or ']', got %q", c.s[j:j+1])
			}
			c.pos++
		}

		v, err := parseIndexedValue(c, maxDepth, p)
		if err != nil {
			if i == 0 && err == errEndOfArray {
				cached := c.s[start : int(c.index[c.pos-1])+1]
				result := make([]Value, 1)
				result[0] = makeStringValue(cached)
				return makeArrayValue(result), nil
			}
			p.maxValues = max(p.maxValues, len(p.values))
			p.values = p.values[:base]
			if err == errEndOfArray {
				return Value{}, fmt.Errorf("unexpected ']' after ','")
			}
			return Value{}, err
		}
		p.values = append(p.values, v)
	}
}

func parseIndexedObject(c *indexCursor, start, maxDepth int, p *parser) (Value, error) {
	if maxDepth == 0 {
		// Lazy object: skip to the matching close brace by scanning the
		// index. Braces inside strings are never emitted, so a simple
		// depth counter is correct.
		depth := 1
		for depth > 0 {
			if c.pos >= len(c.index) {
				return Value{}, errUnexpectedEndOfObject
			}
			switch c.s[c.index[c.pos]] {
			case '{':
				depth++
			case '}':
				depth--
			}
			c.pos++
		}
		json := c.s[start : int(c.index[c.pos-1])+1]
		return makeUnparsedObjectValue(json), nil
	}

	maxDepth--
	if p == nil {
		p = getParser()
		defer putParser(p)
	}
	base := len(p.fields)

	for i := 0; ; i++ {
		if c.pos >= len(c.index) {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, errUnexpectedEndOfObject
		}
		j := c.index[c.pos]
		if c.s[j] == '}' {
			c.pos++
			cached := c.s[start : j+1]
			n := len(p.fields) - base
			result := make([]field, n+1)
			copy(result[1:], p.fields[base:])
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]

			fields := result[1:]
			if n > smallObjectFields {
				hashes := p.allocTags(n)
				for i := range fields {
					hashes[i] = hashKey(fields[i].k)
				}
				result[0].k = unsafe.String(unsafe.SliceData(hashes), n)
			}

			result[0].v = makeStringValue(cached)
			return makeObjectValue(result), nil
		}

		if i != 0 {
			if c.s[j] != ',' {
				p.maxFields = max(p.maxFields, len(p.fields))
				p.fields = p.fields[:base]
				return Value{}, fmt.Errorf("expected ',' or '}', got %q", c.s[j:j+1])
			}
			c.pos++
			if c.pos >= len(c.index) {
				p.maxFields = max(p.maxFields, len(p.fields))
				p.fields = p.fields[:base]
				return Value{}, errUnexpectedEndOfObject
			}
			j = c.index[c.pos]
		}

		if c.s[j] != '"' {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, fmt.Errorf("invalid key at offset %d", j)
		}
		token, err := c.stringToken()
		if err != nil {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, err
		}
		key, err := Unquote(token)
		if err != nil {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, fmt.Errorf("invalid key: %q: %w", token, err)
		}

		if c.pos >= len(c.index) {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, errUnexpectedEndOfObject
		}
		if k := c.index[c.pos]; c.s[k] != ':' {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, fmt.Errorf("%q → expected ':', got %q", key, c.s[k:k+1])
		}
		c.pos++

		val, err := parseIndexedValue(c, maxDepth, p)
		if err != nil {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, fmt.Errorf("%q → %w", key, err)
		}
		p.fields = append(p.fields, field{k: key, v: val})
	}
}
