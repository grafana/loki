package jsonparser

import (
	"bytes"
	"fmt"
)

// Config controls opt-in parsing extensions. The zero value is strict and
// accepts only RFC 8259 JSON strings and escapes.
// SYS-REQ-115
type Config struct {
	AllowSingleQuotes   bool // #160: accept 'key':'value' alongside "key":"value"
	AllowUnknownEscapes bool // #115: accept unknown escape sequences instead of erroring
	MaxBufferSize       int  // ReaderParser sliding-window target; zero uses the 64 MiB default
}

// DefaultConfig is the strict parser configuration used by package-level
// functions.
// SYS-REQ-115
var DefaultConfig = Config{}

// Lenient accepts single-quoted strings and unknown escape sequences.
// SYS-REQ-115
var Lenient = Config{
	AllowSingleQuotes:   true,
	AllowUnknownEscapes: true,
}

// Get returns the value addressed by keys using c's parsing options.
// SYS-REQ-115
func (c Config) Get(data []byte, keys ...string) ([]byte, ValueType, int, error) {
	value, dataType, _, endOffset, err := internalGetConfig(c, data, keys...)
	return value, dataType, endOffset, err
}

// GetString returns the decoded string addressed by keys using c's parsing
// options.
// SYS-REQ-115
func (c Config) GetString(data []byte, keys ...string) (string, error) {
	value, dataType, _, err := c.Get(data, keys...)
	if err != nil {
		return "", err
	}

	if dataType != String {
		if dataType == Null {
			return "", NullValueError
		}
		return "", fmt.Errorf("Value is not a string: %s", string(value))
	}

	if bytes.IndexByte(value, '\\') == -1 {
		return string(value), nil
	}

	var stackbuf [unescapeStackBufSize]byte
	unescaped, err := unescapeConfig(c, value, stackbuf[:])
	if err != nil {
		return "", MalformedValueError
	}
	return string(unescaped), nil
}

// Set replaces the value addressed by keys using c's parsing options.
// SYS-REQ-115
func (c Config) Set(data []byte, value []byte, keys ...string) ([]byte, error) {
	return setConfig(c, data, value, keys...)
}

// Delete removes the value addressed by keys using c's parsing options.
// SYS-REQ-115
func (c Config) Delete(data []byte, keys ...string) []byte {
	result, _ := deleteFoundConfig(c, data, keys...)
	return result
}

// ArrayEach iterates over the addressed array using c's parsing options.
// SYS-REQ-115
func (c Config) ArrayEach(data []byte, cb func([]byte, ValueType, int, error), keys ...string) (int, error) {
	return arrayEachConfig(c, data, cb, keys...)
}

// ObjectEach iterates over the addressed object using c's parsing options.
// SYS-REQ-115
func (c Config) ObjectEach(data []byte, cb func([]byte, []byte, ValueType, int) error, keys ...string) error {
	return objectEachConfig(c, data, cb, keys...)
}
