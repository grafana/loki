package jsonlite

import (
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"unsafe"
)

const (
	// DefaultMaxDepth is the default maximum depth for parsing JSON objects.
	DefaultMaxDepth = 100
)

var (
	errEndOfObject           = errors.New("}")
	errEndOfArray            = errors.New("]")
	errUnexpectedEndOfObject = errors.New("unexpected end of object")
	errUnexpectedEndOfArray  = errors.New("unexpected end of array")
)

// whitespaceMap is a 256-bit lookup table for ASCII whitespace characters.
// Bit i is set if byte i is whitespace (space, tab, newline, carriage return).
var whitespaceMap = func() [4]uint64 {
	var m [4]uint64
	for _, c := range []byte{' ', '\t', '\n', '\r'} {
		m[c/64] |= 1 << (c % 64)
	}
	return m
}()

// isWhitespace returns true if c is a JSON whitespace character.
func isWhitespace(c byte) bool {
	return (whitespaceMap[c/64] & (1 << (c % 64))) != 0
}

// delimiterMap is a 256-bit lookup table for JSON delimiters and whitespace.
// Used to quickly find the end of numbers/literals.
var delimiterMap = func() [4]uint64 {
	var m [4]uint64
	for _, c := range []byte{' ', '\t', '\n', '\r', '[', ']', '{', '}', ':', ',', '"'} {
		m[c/64] |= 1 << (c % 64)
	}
	return m
}()

// isDelimiter returns true if c is a JSON delimiter or whitespace.
func isDelimiter(c byte) bool {
	return (delimiterMap[c/64] & (1 << (c % 64))) != 0
}

// Tokenizer is a JSON tokenizer that splits input into tokens.
// It skips whitespace and returns individual JSON tokens one at a time.
type Tokenizer struct {
	json string
}

// Tokenize creates a new Tokenizer for the given JSON string.
func Tokenize(json string) *Tokenizer {
	return &Tokenizer{json: json}
}

// Next returns the next token from the input.
// Returns an empty string and false when there are no more tokens.
func (t *Tokenizer) Next() (token string, ok bool) {
	token, t.json, ok = nextToken(t.json)
	return token, ok
}

// nextToken extracts the next JSON token from s.
// Returns the token, the remaining string after the token, and whether a token was found.
// All values are kept in registers - no heap allocation for tokenizer state.
func nextToken(s string) (token, rest string, ok bool) {
	// Skip leading whitespace using lookup table
	switch {
	case len(s) == 0:
		return "", "", false
	case s[0] <= ' ':
		for isWhitespace(s[0]) {
			if s = s[1:]; len(s) == 0 {
				return "", "", false
			}
		}
	}

	switch s[0] {
	case '"':
		// Find closing quote, handling escapes
		j := 1
		for {
			k := strings.IndexByte(s[j:], '"')
			if k < 0 {
				return s, "", true
			}
			j += k + 1
			// Count preceding backslashes to check if quote is escaped
			n := 0
			for i := j - 2; i > 0 && s[i] == '\\'; i-- {
				n++
			}
			if n%2 == 0 {
				return s[:j], s[j:], true
			}
		}
	case ',', ':', '[', ']', '{', '}':
		return s[:1], s[1:], true
	default:
		// Numbers and literals: scan until delimiter using lookup table
		j := 1
		for j < len(s) && !isDelimiter(s[j]) {
			j++
		}
		return s[:j], s[j:], true
	}
}

// parser holds scratch stacks shared across the whole parse. Container
// parsing appends to these stacks and copies completed containers into
// exact-size allocations, avoiding a scratch allocation per container.
type parser struct {
	values []Value
	fields []field
	// tags is the block object hash indexes are bump-allocated from. Unlike
	// values and fields it is not scratch: the completed objects alias it, so
	// it is handed off at the end of the parse rather than reused.
	tags []byte
	// high-water marks: the largest lengths reached during this parse,
	// so putParser only clears entries that were actually written.
	maxValues int
	maxFields int
}

var parserPool = sync.Pool{
	New: func() any {
		return &parser{
			values: make([]Value, 0, 64),
			fields: make([]field, 0, 64),
		}
	},
}

func getParser() *parser { return parserPool.Get().(*parser) }

func putParser(p *parser) {
	// Clear only the entries written during this parse so pooled parsers
	// don't retain pointers into previously parsed documents.
	clear(p.values[:min(p.maxValues, cap(p.values))])
	clear(p.fields[:min(p.maxFields, cap(p.fields))])
	p.values = p.values[:0]
	p.fields = p.fields[:0]
	// The tag block is aliased by the objects this parse produced, so it must
	// be released rather than reused: writing into it again would mutate the
	// strings those objects already hold.
	p.tags = nil
	p.maxValues = 0
	p.maxFields = 0
	parserPool.Put(p)
}

// indexedParseThreshold is the document size above which Parse uses the
// structural-index parser when the vectorized stage 1 is available. Below
// this size the classic tokenizer is faster.
const indexedParseThreshold = 512

// ParseMaxDepth parses JSON data with a maximum nesting depth for objects.
// Objects at maxDepth <= 0 are stored unparsed and will be lazily parsed
// when accessed via Lookup(), Array(), or Object() methods.
// Depth is only decremented for objects, not arrays.
// Returns an error if the JSON is malformed or empty.
//
// The input is treated as opaque bytes: string values are not required to be
// valid UTF-8 and are preserved as-is. Callers that need the RFC 8259 UTF-8
// requirement can validate the document upfront with the utf8 subpackage.
func ParseMaxDepth(data string, maxDepth int) (*Value, error) {
	if simdStage1() && len(data) >= indexedParseThreshold {
		return parseIndexed(data, maxDepth)
	}
	// A nil parser is passed down: parseArray and parseObject acquire the
	// pooled scratch stacks on first use, so documents whose root is a
	// primitive never pay the pool round-trip.
	v, rest, err := parseValue(data, max(0, maxDepth), nil)
	if err != nil {
		return nil, err
	}
	// Check for trailing content after the root value
	if extra, _, ok := nextToken(rest); ok {
		return nil, fmt.Errorf("unexpected token after root value: %q", extra)
	}
	return &v, nil
}

// Parse parses JSON data and returns a pointer to the root Value.
// Returns an error if the JSON is malformed or empty.
func Parse(data string) (*Value, error) { return ParseMaxDepth(data, DefaultMaxDepth) }

// ParseSeq parses a sequence of JSON values from the input string.
// It supports both JSON arrays (input starting with '[') and JSON Lines
// (newline-separated values). Returns an iterator yielding each value.
func ParseSeq(json string) iter.Seq2[*Value, error] {
	return func(yield func(*Value, error) bool) {
		token, _, ok := nextToken(json)
		if !ok {
			return
		}
		if token == "[" {
			v, err := Parse(json)
			if err != nil {
				yield(nil, err)
				return
			}
			for elem := range v.Array {
				if !yield(elem, nil) {
					return
				}
			}
			return
		}
		remaining := json
		p := getParser()
		defer putParser(p)
		for {
			v, rest, err := parseValue(remaining, DefaultMaxDepth, p)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(&v, nil) {
				return
			}
			remaining = rest
			if _, _, ok := nextToken(remaining); !ok {
				return
			}
		}
	}
}

// parseValue parses a JSON value from s.
// Returns the parsed value, the remaining unparsed string, and any error.
// The string is passed by value to keep it in registers.
func parseValue(s string, maxDepth int, p *parser) (Value, string, error) {
	token, rest, ok := nextToken(s)
	if !ok {
		return Value{}, rest, errUnexpectedEndOfObject
	}
	switch token[0] {
	case 'n':
		if token != "null" {
			return Value{}, rest, fmt.Errorf("invalid token: %q", token)
		}
		return makeNullValue(token[:4]), rest, nil
	case 't':
		if token != "true" {
			return Value{}, rest, fmt.Errorf("invalid token: %q", token)
		}
		return makeTrueValue(token[:4]), rest, nil
	case 'f':
		if token != "false" {
			return Value{}, rest, fmt.Errorf("invalid token: %q", token)
		}
		return makeFalseValue(token[:5]), rest, nil
	case '"':
		// Validate the quoted string but store the quoted token
		if !validString(token) {
			return Value{}, rest, fmt.Errorf("invalid token: %q", token)
		}
		return makeStringValue(token), rest, nil
	case '[':
		return parseArray(s, rest, maxDepth, p)
	case '{':
		return parseObject(s, rest, maxDepth, p)
	case ']':
		return Value{}, rest, errEndOfArray
	case '}':
		return Value{}, rest, errEndOfObject
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if !validNumber(token) {
			return Value{}, rest, fmt.Errorf("invalid number: %q", token)
		}
		return makeNumberValue(token), rest, nil
	default:
		return Value{}, rest, fmt.Errorf("invalid token: %q", token)
	}
}

func parseArray(start, json string, maxDepth int, p *parser) (Value, string, error) {
	// The root container acquires the pooled scratch and owns its return;
	// nested containers receive the parser from their parent.
	if p == nil {
		p = getParser()
		defer putParser(p)
	}
	base := len(p.values)

	for i := 0; ; i++ {
		if i != 0 {
			token, rest, ok := nextToken(json)
			if !ok {
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return Value{}, json, errUnexpectedEndOfArray
			}
			if token == "]" {
				cached := start[:len(start)-len(rest)]
				result := make([]Value, len(p.values)-base+1)
				result[0] = makeStringValue(cached)
				copy(result[1:], p.values[base:])
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return makeArrayValue(result), rest, nil
			}
			if token != "," {
				p.maxValues = max(p.maxValues, len(p.values))
				p.values = p.values[:base]
				return Value{}, json, fmt.Errorf("expected ',' or ']', got %q", token)
			}
			json = rest
		}

		v, rest, err := parseValue(json, maxDepth, p)
		if err != nil {
			if i == 0 && err == errEndOfArray {
				cached := start[:len(start)-len(rest)]
				result := make([]Value, 1)
				result[0] = makeStringValue(cached)
				return makeArrayValue(result), rest, nil
			}
			p.maxValues = max(p.maxValues, len(p.values))
			p.values = p.values[:base]
			if err == errEndOfArray {
				return Value{}, json, fmt.Errorf("unexpected ']' after ','")
			}
			return Value{}, json, err
		}
		json = rest
		p.values = append(p.values, v)
	}
}

// smallObjectFields is the field count at or below which objects skip
// building the 1-byte hash index; Lookup falls back to a linear key scan,
// which is faster than hashing at these sizes and saves the tag bytes and
// per-key hashKey calls at parse time.
//
// The crossover is where a linear scan reaches the indexed lookup's flat
// floor, and it is set by the tag scan and its extra indirection rather than
// by the hash: making hashKey 2.5x cheaper does not move it. Measured with
// BenchmarkThreshParseLookup, indexing costs 12-16% at 6 fields, 5-10% at 8,
// and starts paying by 12.
const smallObjectFields = 8

func parseObject(start, json string, maxDepth int, p *parser) (Value, string, error) {
	if maxDepth == 0 {
		depth, remain := 1, json
		for depth > 0 {
			token, next, ok := nextToken(remain)
			if !ok {
				return Value{}, remain, errUnexpectedEndOfObject
			}
			remain = next
			switch token {
			case "{":
				depth++
			case "}":
				depth--
			}
		}
		json := start[:len(start)-len(remain)]
		return makeUnparsedObjectValue(json), remain, nil
	}

	maxDepth--
	// The root container acquires the pooled scratch and owns its return;
	// nested containers receive the parser from their parent. Acquired after
	// the lazy-object path above, which uses no scratch.
	if p == nil {
		p = getParser()
		defer putParser(p)
	}
	base := len(p.fields)

	for i := 0; ; i++ {
		token, rest, ok := nextToken(json)
		if !ok {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, json, errUnexpectedEndOfObject
		}
		if token == "}" {
			cached := start[:len(start)-len(rest)]
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
			return makeObjectValue(result), rest, nil
		}
		json = rest

		if i != 0 {
			if token != "," {
				p.maxFields = max(p.maxFields, len(p.fields))
				p.fields = p.fields[:base]
				return Value{}, json, fmt.Errorf("expected ',' or '}', got %q", token)
			}
			token, rest, ok = nextToken(json)
			if !ok {
				p.maxFields = max(p.maxFields, len(p.fields))
				p.fields = p.fields[:base]
				return Value{}, json, errUnexpectedEndOfObject
			}
			json = rest
		}

		key, err := Unquote(token)
		if err != nil {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, json, fmt.Errorf("invalid key: %q: %w", token, err)
		}

		token, rest, ok = nextToken(json)
		if !ok {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, json, errUnexpectedEndOfObject
		}
		if token != ":" {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, json, fmt.Errorf("%q → expected ':', got %q", key, token)
		}
		json = rest

		val, rest, err := parseValue(json, maxDepth, p)
		if err != nil {
			p.maxFields = max(p.maxFields, len(p.fields))
			p.fields = p.fields[:base]
			return Value{}, json, fmt.Errorf("%q → %w", key, err)
		}
		json = rest
		p.fields = append(p.fields, field{k: key, v: val})
	}
}
