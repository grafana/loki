package jsonparser

import (
	"errors"
	"strings"
)

var (
	errEmptyPath       = errors.New("jsonparser: path must not be empty")
	errMalformedPath   = errors.New("jsonparser: malformed path")
	errUnterminatedKey = errors.New("jsonparser: unterminated quoted key")
)

// ParsePath converts a JSONPath-style path into the path components accepted
// by Get, Set, Delete, ArrayEach, and EachKey.
// SYS-REQ-114
func ParsePath(jsonPath string) ([]string, error) {
	if jsonPath == "" {
		return nil, errEmptyPath
	}

	switch {
	case jsonPath == "$":
		return []string{}, nil
	case strings.HasPrefix(jsonPath, "$."):
		jsonPath = jsonPath[2:]
	case strings.HasPrefix(jsonPath, "$["):
		jsonPath = jsonPath[1:]
	case jsonPath[0] == '$':
		return nil, errMalformedPath
	}

	if jsonPath == "" {
		return nil, errMalformedPath
	}

	// A path component is either a dot-delimited key or bracket notation.
	// Counting both separators gives an exact capacity for ordinary paths and
	// a safe upper bound for quoted keys containing dots or brackets.
	parts := make([]string, 0, 1+strings.Count(jsonPath, ".")+strings.Count(jsonPath, "["))

	for pos := 0; pos < len(jsonPath); {
		switch jsonPath[pos] {
		case '.':
			return nil, errMalformedPath
		case '"':
			key, next, err := parseQuotedPathKey(jsonPath, pos)
			if err != nil {
				return nil, err
			}
			parts = append(parts, key)
			pos = next
		case '[':
			// A root array path or a bracket immediately following a dot has
			// no key component before its index.
		default:
			start := pos
			for pos < len(jsonPath) && jsonPath[pos] != '.' && jsonPath[pos] != '[' {
				if jsonPath[pos] == ']' || jsonPath[pos] == '"' {
					return nil, errMalformedPath
				}
				pos++
			}
			if start == pos {
				return nil, errMalformedPath
			}
			parts = append(parts, jsonPath[start:pos])
		}

		for pos < len(jsonPath) && jsonPath[pos] == '[' {
			component, next, err := parseBracketPathComponent(jsonPath, pos)
			if err != nil {
				return nil, err
			}
			parts = append(parts, component)
			pos = next
		}

		if pos == len(jsonPath) {
			break
		}
		if jsonPath[pos] != '.' {
			return nil, errMalformedPath
		}

		pos++
		if pos == len(jsonPath) {
			return nil, errMalformedPath
		}
	}

	if len(parts) == 0 {
		return nil, errMalformedPath
	}
	return parts, nil
}

// SYS-REQ-114
func parseQuotedPathKey(path string, start int) (string, int, error) {
	contentStart := start + 1
	for pos := contentStart; pos < len(path); pos++ {
		switch path[pos] {
		case '\\':
			// Skip the escaped byte while locating the closing quote. Unescape
			// below performs complete JSON escape validation.
			pos++
			if pos >= len(path) {
				return "", 0, errUnterminatedKey
			}
		case '"':
			content := path[contentStart:pos]
			if strings.IndexByte(content, '\\') == -1 {
				return content, pos + 1, nil
			}

			unescaped, err := Unescape([]byte(content), nil)
			if err != nil {
				return "", 0, errMalformedPath
			}
			return string(unescaped), pos + 1, nil
		default:
			if path[pos] < 0x20 {
				return "", 0, errMalformedPath
			}
		}
	}

	return "", 0, errUnterminatedKey
}

// SYS-REQ-114
func parseBracketPathComponent(path string, start int) (string, int, error) {
	end := start + 1
	for end < len(path) && path[end] != ']' {
		if path[end] == '[' || path[end] == '"' {
			return "", 0, errMalformedPath
		}
		end++
	}
	if end == len(path) || end == start+1 {
		return "", 0, errMalformedPath
	}

	if path[start+1] == '*' {
		if end != start+2 {
			return "", 0, errMalformedPath
		}
	} else {
		for pos := start + 1; pos < end; pos++ {
			if path[pos] < '0' || path[pos] > '9' {
				return "", 0, errMalformedPath
			}
		}
	}

	return path[start : end+1], end + 1, nil
}

// CompiledPath stores a parsed path for repeated operations.
// SYS-REQ-114
type CompiledPath struct {
	parts []string
}

// CompilePath parses jsonPath once for reuse.
// SYS-REQ-114
func CompilePath(jsonPath string) (CompiledPath, error) {
	parts, err := ParsePath(jsonPath)
	if err != nil {
		return CompiledPath{}, err
	}
	return CompiledPath{parts: parts}, nil
}

// Get resolves the compiled path in data.
// Verifies: SYS-REQ-001 (Get)
// SYS-REQ-114
func (c CompiledPath) Get(data []byte) ([]byte, ValueType, int, error) {
	return Get(data, c.parts...)
}

// GetString resolves the compiled path and returns its string value.
// SYS-REQ-114
func (c CompiledPath) GetString(data []byte) (string, error) {
	return GetString(data, c.parts...)
}

// GetInt resolves the compiled path and returns its integer value.
// SYS-REQ-114
func (c CompiledPath) GetInt(data []byte) (int64, error) {
	return GetInt(data, c.parts...)
}

// Set writes value at the compiled path.
// Verifies: SYS-REQ-009 (Set)
// SYS-REQ-114
func (c CompiledPath) Set(data []byte, value []byte) ([]byte, error) {
	return Set(data, value, c.parts...)
}

// Delete removes the value at the compiled path.
// SYS-REQ-114
func (c CompiledPath) Delete(data []byte) []byte {
	return Delete(data, c.parts...)
}

// ArrayEach iterates over the array at the compiled path.
// SYS-REQ-114
func (c CompiledPath) ArrayEach(data []byte, cb func([]byte, ValueType, int, error)) (int, error) {
	return ArrayEach(data, cb, c.parts...)
}

// EachKey resolves the compiled path using EachKey.
// SYS-REQ-114
func (c CompiledPath) EachKey(data []byte, cb func(int, []byte, ValueType, error)) error {
	EachKey(data, cb, c.parts)
	return nil
}

// Parts returns a copy of the compiled path components.
// SYS-REQ-114
func (c CompiledPath) Parts() []string {
	parts := make([]string, len(c.parts))
	copy(parts, c.parts)
	return parts
}
