package jsonlite

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// Iterator provides a streaming interface for traversing JSON values.
// It automatically handles control tokens (braces, brackets, colons, commas)
// and presents only the logical JSON values to the caller.
type Iterator struct {
	tokens   Tokenizer
	json     string // Original JSON for computing pre-token positions
	token    string
	kind     Kind
	key      string
	err      error
	state    []byte // stack of states: 'a' for array, 'o' for object (expecting key), 'v' for object (expecting value)
	bytes    [16]byte
	consumed bool // whether the current value has been consumed
}

// Iterate creates a new Iterator for the given JSON string.
func Iterate(json string) *Iterator {
	it := &Iterator{
		tokens: Tokenizer{json: json},
		json:   json, // Store original JSON
	}
	it.state = it.bytes[:0]
	return it
}

// Reset resets the iterator to parse a new JSON string.
func (it *Iterator) Reset(json string) {
	it.tokens = Tokenizer{json: json}
	it.json = json
	it.token = ""
	it.kind = 0
	it.key = ""
	it.err = nil
	it.consumed = false
}

// Next advances the iterator to the next JSON value.
// Returns true if there is a value to process, false when done or on error.
func (it *Iterator) Next() bool {
	for {
		token, ok := it.tokens.Next()
		if !ok {
			if len(it.state) > 0 {
				if it.top() == 'a' {
					it.err = errUnexpectedEndOfArray
				} else {
					it.err = errUnexpectedEndOfObject
				}
			}
			return false
		}

		if len(it.state) > 0 {
			s := it.top()
			switch s {
			case 'a': // in array, expecting value or ]
				if token == "]" {
					it.pop()
					continue
				}
				if token == "," {
					continue
				}
			case 'o': // in object, expecting key or }
				if token == "}" {
					it.pop()
					continue
				}
				if token == "," {
					continue
				}
				key, err := Unquote(token)
				if err != nil {
					it.setErrorf("invalid key: %q: %w", token, err)
					return false
				}
				it.setKey(key)
				colon, ok := it.tokens.Next()
				if !ok {
					it.err = errUnexpectedEndOfObject
					return false
				}
				if colon != ":" {
					it.setErrorf("expected ':', got %q", colon)
					return false
				}
				// Change state to expect value
				it.set('v')
				continue
			case 'v': // in object, expecting value
				// Change state back to expect key/}
				it.set('o')
			}
		}

		return it.setToken(token)
	}
}

func (it *Iterator) push(state byte) {
	it.state = append(it.state, state)
}

func (it *Iterator) pop() {
	it.state = it.state[:len(it.state)-1]
}

func (it *Iterator) top() byte {
	return it.state[len(it.state)-1]
}

func (it *Iterator) set(state byte) {
	it.state[len(it.state)-1] = state
}

func (it *Iterator) setError(err error) {
	it.err = err
}

func (it *Iterator) setErrorf(msg string, args ...any) {
	it.setError(fmt.Errorf(msg, args...))
}

func (it *Iterator) setKey(key string) {
	it.key = key
}

func (it *Iterator) setToken(token string) bool {
	kind, err := tokenKind(token)
	it.token = token
	it.kind = kind
	it.err = err
	it.consumed = false

	if err != nil {
		return false
	}

	switch kind {
	case Array:
		it.push('a')
	case Object:
		it.push('o')
	}

	return true
}

func (it *Iterator) skipArray() {
	depth := 1
	for depth > 0 {
		token, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfArray)
			return
		}
		switch token {
		case "[":
			depth++
		case "]":
			depth--
		}
	}
	it.pop()
}

func (it *Iterator) skipObject() {
	depth := 1
	for depth > 0 {
		token, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfObject)
			return
		}
		switch token {
		case "{":
			depth++
		case "}":
			depth--
		}
	}
	it.pop()
}

func tokenKind(token string) (Kind, error) {
	switch token[0] {
	case 'n':
		if token != "null" {
			return Null, fmt.Errorf("invalid token: %q", token)
		}
		return Null, nil
	case 't':
		if token != "true" {
			return Null, fmt.Errorf("invalid token: %q", token)
		}
		return True, nil
	case 'f':
		if token != "false" {
			return Null, fmt.Errorf("invalid token: %q", token)
		}
		return False, nil
	case '"':
		return String, nil
	case '[':
		return Array, nil
	case '{':
		return Object, nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if !validNumber(token) {
			return Number, fmt.Errorf("invalid number: %q", token)
		}
		return Number, nil
	default:
		return Null, fmt.Errorf("invalid token: %q", token)
	}
}

// Kind returns the kind of the current value.
func (it *Iterator) Kind() Kind { return it.kind }

// Key returns the object key for the current value, if inside an object.
// Returns an empty string if not inside an object or at the top level.
func (it *Iterator) Key() string { return it.key }

// Err returns any error that occurred during iteration.
func (it *Iterator) Err() error { return it.err }

// Depth returns the current nesting depth (0 at top level).
func (it *Iterator) Depth() int { return len(it.state) }

// Value parses and returns the current value.
// For arrays and objects, this consumes all nested tokens and returns the
// complete parsed structure.
func (it *Iterator) Value() (*Value, error) {
	val, err := it.value()
	return &val, err
}

func (it *Iterator) value() (Value, error) {
	if it.err != nil {
		return Value{}, it.err
	}

	switch it.kind {
	case Null, True, False, Number:
		return makeValue(it.kind, it.token), nil
	case String:
		// Validate the quoted string but store the quoted token
		if !validString(it.token) {
			return Value{}, fmt.Errorf("invalid string: %q", it.token)
		}
		return makeStringValue(it.token), nil
	case Array:
		delimi := len(it.token)
		offset := len(it.json) - len(it.tokens.json) - delimi
		val, rest, err := parseArray(it.json[offset:], it.tokens.json, DefaultMaxDepth)
		it.tokens.json, it.consumed = rest, true
		if err != nil {
			it.setError(err)
		}
		it.pop()
		return val, err
	case Object:
		delimi := len(it.token)
		offset := len(it.json) - len(it.tokens.json) - delimi
		val, rest, err := parseObject(it.json[offset:], it.tokens.json, DefaultMaxDepth)
		it.tokens.json, it.consumed = rest, true
		if err != nil {
			it.setError(err)
		}
		it.pop()
		return val, err
	default:
		return Value{}, fmt.Errorf("unexpected kind: %v", it.kind)
	}
}

// Null returns true if the current value is null.
func (it *Iterator) Null() bool { return it.kind == Null }

// Bool returns the current value as a boolean.
// Returns false for null values.
// Returns an error if the value is not a boolean, null, or a string that can be parsed as a boolean.
func (it *Iterator) Bool() (bool, error) {
	switch it.kind {
	case Null, False:
		return false, nil
	case True:
		return true, nil
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return false, fmt.Errorf("invalid string: %q", it.token)
		}
		return strconv.ParseBool(s)
	default:
		return false, fmt.Errorf("cannot convert %v to bool", it.kind)
	}
}

// Int returns the current value as a signed 64-bit integer.
// Returns 0 for null values.
// Returns an error if the value is not a number, null, or a string that can be parsed as an integer.
func (it *Iterator) Int() (int64, error) {
	switch it.kind {
	case Null:
		return 0, nil
	case Number:
		return strconv.ParseInt(it.token, 10, 64)
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return 0, fmt.Errorf("invalid string: %q", it.token)
		}
		return strconv.ParseInt(s, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %v to int", it.kind)
	}
}

// Float returns the current value as a 64-bit floating point number.
// Returns 0 for null values.
// Returns an error if the value is not a number, null, or a string that can be parsed as a float.
func (it *Iterator) Float() (float64, error) {
	switch it.kind {
	case Null:
		return 0, nil
	case Number:
		return strconv.ParseFloat(it.token, 64)
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return 0, fmt.Errorf("invalid string: %q", it.token)
		}
		return strconv.ParseFloat(s, 64)
	default:
		return 0, fmt.Errorf("cannot convert %v to float", it.kind)
	}
}

// String returns the current value as a string.
// Returns "" for null values.
// Returns an error if the value is not a string or null.
func (it *Iterator) String() (string, error) {
	switch it.kind {
	case Null:
		return "", nil
	case String:
		return Unquote(it.token)
	default:
		return "", fmt.Errorf("cannot convert %v to string", it.kind)
	}
}

// Duration returns the current value as a time.Duration.
// Returns 0 for null values.
// For numbers, the value is interpreted as seconds.
// For strings, the value is parsed using time.ParseDuration.
// Returns an error if the value cannot be converted to a duration.
func (it *Iterator) Duration() (time.Duration, error) {
	switch it.kind {
	case Null:
		return 0, nil
	case Number:
		f, err := strconv.ParseFloat(it.token, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(f * float64(time.Second)), nil
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return 0, fmt.Errorf("invalid string: %q", it.token)
		}
		return time.ParseDuration(s)
	default:
		return 0, fmt.Errorf("cannot convert %v to duration", it.kind)
	}
}

// Time returns the current value as a time.Time.
// Returns the zero time for null values.
// For numbers, the value is interpreted as seconds since Unix epoch.
// For strings, the value is parsed using RFC3339 format.
// Returns an error if the value cannot be converted to a time.
func (it *Iterator) Time() (time.Time, error) {
	switch it.kind {
	case Null:
		return time.Time{}, nil
	case Number:
		f, err := strconv.ParseFloat(it.token, 64)
		if err != nil {
			return time.Time{}, err
		}
		sec, frac := math.Modf(f)
		return time.Unix(int64(sec), int64(frac*1e9)).UTC(), nil
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid string: %q", it.token)
		}
		return time.Parse(time.RFC3339, s)
	default:
		return time.Time{}, fmt.Errorf("cannot convert %v to time", it.kind)
	}
}

// Object iterates over the key-value pairs of the current object.
// The iterator yields the key for each field, and the Iterator is positioned
// on the field's value. Call Kind(), Value(), Object(), or Array() to process
// the value. If the value is not consumed before the next iteration, it will
// be automatically skipped.
// For null values, no iterations occur.
//
// Must only be called when Kind() == Object or Kind() == Null.
func (it *Iterator) Object(yield func(string, error) bool) {
	if it.kind == Null {
		return // null is treated as empty object
	}
	it.consumed = true // mark the object itself as consumed
	for i := 0; ; i++ {
		// Auto-consume the previous value if it wasn't consumed
		if !it.consumed {
			switch it.kind {
			case Array:
				it.consumed = true
				it.skipArray()
			case Object:
				it.consumed = true
				it.skipObject()
			}
		}

		token, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfObject)
			yield("", it.err)
			return
		}

		if token == "}" {
			it.pop()
			return
		}

		if i != 0 {
			if token != "," {
				it.setErrorf("expected ',', got %q", token)
				yield("", it.err)
				return
			}
			token, ok = it.tokens.Next()
			if !ok {
				it.setError(errUnexpectedEndOfObject)
				yield("", it.err)
				return
			}
		}

		key, err := Unquote(token)
		if err != nil {
			it.setErrorf("invalid key: %q: %w", token, err)
			yield("", it.err)
			return
		}

		colon, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfObject)
			yield("", it.err)
			return
		}
		if colon != ":" {
			it.setErrorf("expected ':', got %q", colon)
			yield("", it.err)
			return
		}

		value, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfObject)
			yield("", it.err)
			return
		}

		it.setKey(key)
		it.setToken(value)

		if !yield(key, nil) {
			return
		}
	}
}

// Array iterates over the elements of the current array.
// The iterator yields the index for each element, and the Iterator is
// positioned on the element's value. Call Kind(), Value(), Object(), or
// Array() to process the value. If the value is not consumed before the
// next iteration, it will be automatically skipped.
// For null values, no iterations occur.
//
// Must only be called when Kind() == Array or Kind() == Null.
func (it *Iterator) Array(yield func(int, error) bool) {
	if it.kind == Null {
		return // null is treated as empty array
	}
	it.consumed = true // mark the array itself as consumed
	for i := 0; ; i++ {
		// Auto-consume the previous value if it wasn't consumed
		if !it.consumed {
			switch it.kind {
			case Array:
				it.consumed = true
				it.skipArray()
			case Object:
				it.consumed = true
				it.skipObject()
			}
		}

		token, ok := it.tokens.Next()
		if !ok {
			it.setError(errUnexpectedEndOfArray)
			yield(i, it.err)
			return
		}

		if token == "]" {
			it.pop()
			return
		}

		if i != 0 {
			if token != "," {
				it.setErrorf("expected ',', got %q", token)
				yield(i, it.err)
				return
			}
			token, ok = it.tokens.Next()
			if !ok {
				it.setError(errUnexpectedEndOfArray)
				yield(i, it.err)
				return
			}
		}

		it.setToken(token)

		if !yield(i, nil) {
			return
		}
	}
}
