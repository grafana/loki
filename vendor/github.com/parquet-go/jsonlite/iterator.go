package jsonlite

import (
	"fmt"
	"iter"
)

// Iterator provides a streaming interface for traversing JSON values.
// It automatically handles control tokens (braces, brackets, colons, commas)
// and presents only the logical JSON values to the caller.
type Iterator struct {
	tokens Tokenizer
	token  string
	kind   Kind
	key    string
	err    error
	state  []byte // stack of states: 'a' for array, 'o' for object (expecting key), 'v' for object (expecting value)
	bytes  [16]byte
}

// Iterate creates a new Iterator for the given JSON string.
func Iterate(json string) *Iterator {
	it := &Iterator{tokens: Tokenizer{json: json}}
	it.state = it.bytes[:0]
	return it
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
func (it *Iterator) Value() (Value, error) {
	if it.err != nil {
		return Value{}, it.err
	}

	switch it.kind {
	case Null:
		return makeNullValue(), nil
	case True:
		return makeTrueValue(), nil
	case False:
		return makeFalseValue(), nil
	case Number:
		return makeNumberValue(it.token), nil
	case String:
		s, err := Unquote(it.token)
		if err != nil {
			return Value{}, fmt.Errorf("invalid string: %q", it.token)
		}
		return makeStringValue(s), nil
	case Array:
		val, rest, err := parseArray(it.tokens.json)
		it.tokens.json = rest
		if err != nil {
			it.setError(err)
		}
		it.pop()
		return val, err
	case Object:
		val, rest, err := parseObject(it.tokens.json)
		it.tokens.json = rest
		if err != nil {
			it.setError(err)
		}
		it.pop()
		return val, err
	default:
		return Value{}, fmt.Errorf("unexpected kind: %v", it.kind)
	}
}

// Object returns an iterator over the key-value pairs of the current object.
// The iterator yields the key for each field, and the Iterator is positioned
// on the field's value. Call Kind(), Value(), Object(), or Array() to process
// the value.
//
// Must only be called when Kind() == Object.
func (it *Iterator) Object() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for i := 0; ; i++ {
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
}

// Array returns an iterator over the elements of the current array.
// The iterator yields the index for each element, and the Iterator is
// positioned on the element's value. Call Kind(), Value(), Object(), or
// Array() to process the value.
//
// Must only be called when Kind() == Array.
func (it *Iterator) Array() iter.Seq2[int, error] {
	return func(yield func(int, error) bool) {
		for i := 0; ; i++ {
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
}
