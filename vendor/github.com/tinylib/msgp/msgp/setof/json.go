package setof

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// isJSONNull reports whether data is the JSON literal "null".
func isJSONNull(data []byte) bool {
	return string(data) == "null"
}

func skipWS(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	return i
}

// jsonArrayIter calls fn for each raw element in a JSON array.
// String elements include their surrounding quotes. Does not allocate.
// Simple string and integer types are supported.
func jsonArrayIter(data []byte, fn func(raw []byte) error) error {
	i := skipWS(data, 0)
	if i >= len(data) || data[i] != '[' {
		return fmt.Errorf("setof: expected '[', got %q", string(data[i:]))
	}
	i = skipWS(data, i+1)
	if i < len(data) && data[i] == ']' {
		return nil
	}
	for {
		i = skipWS(data, i)
		if i >= len(data) {
			return fmt.Errorf("setof: unexpected end of JSON array")
		}
		start := i
		if data[i] == '"' {
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == '"' {
					i++
					break
				}
				i++
			}
		} else {
			for i < len(data) && data[i] != ',' && data[i] != ']' &&
				data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' {
				i++
			}
		}
		if err := fn(data[start:i]); err != nil {
			return err
		}
		i = skipWS(data, i)
		if i >= len(data) {
			return fmt.Errorf("setof: unexpected end of JSON array")
		}
		if data[i] == ']' {
			return nil
		}
		if data[i] != ',' {
			return fmt.Errorf("setof: expected ',' or ']', got '%c'", data[i])
		}
		i++
	}
}

// jsonAppendQuote appends a JSON-encoded string to dst.
// Invalid UTF-8 is replaced with U+FFFD, matching encoding/json behavior.
func jsonAppendQuote(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for _, r := range s {
		if r >= 0x20 && r != '"' && r != '\\' {
			// Any printable rune...
			dst = utf8.AppendRune(dst, r)
			continue
		}
		switch r {
		case '"', '\\':
			dst = append(dst, '\\', byte(r))
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			// reachable only when r < 0x20 (other control chars); always fits in \u00XX
			dst = append(dst, '\\', 'u', '0', '0', hexDigit(byte(r>>4)), hexDigit(byte(r&0xf)))
		}
	}
	dst = append(dst, '"')
	return dst
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}

// jsonParseQuoted parses a JSON string element (including quotes).
func jsonParseQuoted(b []byte) (string, error) {
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return "", fmt.Errorf("setof: invalid JSON string: %s", b)
	}
	inner := b[1 : len(b)-1]
	if bytes.IndexByte(inner, '\\') < 0 {
		return string(inner), nil
	}
	var s string
	err := json.Unmarshal(b, &s)
	return s, err
}

type signedInt interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsignedInt interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type floatNum interface {
	~float32 | ~float64
}

func jsonParseSigned[T signedInt](b []byte, bitSize int) (T, error) {
	v, err := strconv.ParseInt(string(b), 10, bitSize)
	return T(v), err
}

func jsonParseUnsigned[T unsignedInt](b []byte, bitSize int) (T, error) {
	v, err := strconv.ParseUint(string(b), 10, bitSize)
	return T(v), err
}

func jsonParseFloat[T floatNum](b []byte, bitSize int) (T, error) {
	v, err := strconv.ParseFloat(string(b), bitSize)
	return T(v), err
}
