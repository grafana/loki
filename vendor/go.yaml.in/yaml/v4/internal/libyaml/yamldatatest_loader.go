// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// YAML test data loading utilities.
// Provides helper functions for loading and processing YAML test data,
// including scalar coercion.

package libyaml

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// coerceScalar converts a YAML scalar string to an appropriate Go type
func coerceScalar(value string) any {
	// Try bool and null
	switch value {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}

	// Try hex int (0x or 0X prefix) - needed for test data byte arrays
	var intVal int
	if _, err := fmt.Sscanf(strings.ToLower(value), "0x%x", &intVal); err == nil {
		return intVal
	}

	// Try float (must check before int because %d will parse "1.5" as "1")
	if strings.Contains(value, ".") {
		var floatVal float64
		if _, err := fmt.Sscanf(value, "%f", &floatVal); err == nil {
			return floatVal
		}
	}

	// Try decimal int - use int64 to handle large values on 32-bit systems
	var int64Val int64
	if _, err := fmt.Sscanf(value, "%d", &int64Val); err == nil {
		// Return as int if it fits, otherwise int64
		if int64Val == int64(int(int64Val)) {
			return int(int64Val)
		}
		return int64Val
	}

	// Default to string
	return value
}

// LoadYAML parses YAML data using the native libyaml Parser.
// This function is exported so it can be used by other packages for data-driven testing.
// It returns a generic interface{} which is typically:
//   - map[string]interface{} for YAML mappings
//   - []interface{} for YAML sequences
//   - scalar values, resolved according to the following rules:
//   - Booleans: "true" and "false" are returned as bool (true/false).
//   - Nulls: "null" is returned as nil.
//   - Floats: values containing "." are parsed as float64.
//   - Decimal integers: values matching integer format are parsed as int.
//   - All other values are returned as string.
//
// This scalar resolution behavior matches the implementation in coerceScalar.
func LoadYAML(data []byte) (any, error) {
	parser := NewParser()
	parser.SetInputString(data)
	defer parser.Delete()

	type stackEntry struct {
		container any    // map[string]interface{} or []interface{}
		key       string // for maps: current key waiting for value
	}

	var stack []stackEntry
	var root any

	for {
		var event Event
		if err := parser.Parse(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		switch event.Type {
		case STREAM_END_EVENT:
			// End of stream, we're done
			return root, nil

		case STREAM_START_EVENT, DOCUMENT_START_EVENT:
			// Structural markers, no action needed

		case MAPPING_START_EVENT:
			newMap := make(map[string]any)
			stack = append(stack, stackEntry{container: newMap})

		case MAPPING_END_EVENT:
			if len(stack) > 0 {
				popped := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				// Add completed map to parent or set as root
				if len(stack) == 0 {
					root = popped.container
				} else {
					parent := &stack[len(stack)-1]
					if m, ok := parent.container.(map[string]any); ok {
						m[parent.key] = popped.container
						parent.key = "" // Reset key after use
					} else if s, ok := parent.container.([]any); ok {
						parent.container = append(s, popped.container)
					}
				}
			}

		case SEQUENCE_START_EVENT:
			newSlice := make([]any, 0)
			stack = append(stack, stackEntry{container: newSlice})

		case SEQUENCE_END_EVENT:
			if len(stack) > 0 {
				popped := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				// Add completed slice to parent or set as root
				if len(stack) == 0 {
					root = popped.container
				} else {
					parent := &stack[len(stack)-1]
					if m, ok := parent.container.(map[string]any); ok {
						m[parent.key] = popped.container
						parent.key = "" // Reset key after use
					} else if s, ok := parent.container.([]any); ok {
						parent.container = append(s, popped.container)
					}
				}
			}

		case SCALAR_EVENT:
			value := string(event.Value)
			// Only coerce plain (unquoted) scalars
			isQuoted := ScalarStyle(event.Style) != PLAIN_SCALAR_STYLE

			if len(stack) == 0 {
				// Scalar at root level
				if isQuoted {
					root = value
				} else {
					root = coerceScalar(value)
				}
			} else {
				parent := &stack[len(stack)-1]
				if m, ok := parent.container.(map[string]any); ok {
					if parent.key == "" {
						// This scalar is a key - keep as string, don't coerce
						parent.key = value
					} else {
						// This scalar is a value
						if isQuoted {
							m[parent.key] = value
						} else {
							m[parent.key] = coerceScalar(value)
						}
						parent.key = ""
					}
				} else if s, ok := parent.container.([]any); ok {
					// Add to sequence
					if isQuoted {
						parent.container = append(s, value)
					} else {
						parent.container = append(s, coerceScalar(value))
					}
				}
			}

		case DOCUMENT_END_EVENT:
			// Document end marker, continue processing

		case ALIAS_EVENT, TAIL_COMMENT_EVENT:
			// For now, skip aliases and comments (not used in test data)
		}
	}

	return root, nil
}
