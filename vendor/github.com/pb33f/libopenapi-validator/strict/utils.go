// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package strict

import (
	"regexp"
	"strconv"
	"strings"
)

// buildPath creates an instance path by appending a property name to a base path.
// Property names containing dots or brackets use bracket notation for clarity.
//
// Examples:
//   - buildPath("$.body", "name") → "$.body.name"
//   - buildPath("$.body", "a.b") → "$.body['a.b']"
//   - buildPath("$.body", "x[0]") → "$.body['x[0]']"
func buildPath(base, propName string) string {
	if needsBracketNotation(propName) {
		return base + "['" + propName + "']"
	}
	return base + "." + propName
}

// needsBracketNotation returns true if a property name contains characters
// that require bracket notation (dots, brackets).
func needsBracketNotation(name string) bool {
	return strings.ContainsAny(name, ".[]")
}

// buildArrayPath creates an instance path for an array element.
func buildArrayPath(base string, index int) string {
	return base + "[" + strconv.Itoa(index) + "]"
}

// compileIgnorePaths converts glob patterns to compiled regular expressions.
// Supports:
//   - * matches single path segment (no dots or brackets)
//   - ** matches any depth (zero or more segments)
//   - [*] matches any array index
//   - \* escapes literal asterisk
//   - \*\* escapes literal double-asterisk
func compileIgnorePaths(patterns []string) []*regexp.Regexp {
	if len(patterns) == 0 {
		return nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re := compilePattern(pattern)
		if re != nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// compilePattern converts a single glob pattern to a regular expression.
func compilePattern(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}

	var b strings.Builder
	b.WriteString("^")

	i := 0
	for i < len(pattern) {
		c := pattern[i]

		// handle escape sequences
		if c == '\\' && i+1 < len(pattern) {
			next := pattern[i+1]
			if next == '*' {
				// check for escaped **
				if i+2 < len(pattern) && pattern[i+2] == '\\' && i+3 < len(pattern) && pattern[i+3] == '*' {
					b.WriteString(`\*\*`)
					i += 4
					continue
				}
				// escaped single *
				b.WriteString(`\*`)
				i += 2
				continue
			}
			// other escape - include literally
			b.WriteString(regexp.QuoteMeta(string(next)))
			i += 2
			continue
		}

		// handle ** (any depth)
		if c == '*' && i+1 < len(pattern) && pattern[i+1] == '*' {
			// ** matches any sequence of segments including none
			b.WriteString(`.*`)
			i += 2
			continue
		}

		// handle single * (single segment)
		if c == '*' {
			// * matches single path segment (no dots or brackets)
			b.WriteString(`[^.\[\]]+`)
			i++
			continue
		}

		// handle [*] (any array index)
		if c == '[' && i+2 < len(pattern) && pattern[i+1] == '*' && pattern[i+2] == ']' {
			b.WriteString(`\[\d+\]`)
			i += 3
			continue
		}

		// handle special regex characters
		switch c {
		case '.', '[', ']', '(', ')', '{', '}', '+', '?', '^', '$', '|':
			b.WriteString(`\`)
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
		i++
	}

	b.WriteString("$")

	re, _ := regexp.Compile(b.String())
	return re
}

// TruncateValue creates a display-friendly version of a value.
// Long strings are truncated, complex objects show type info.
// This is exported for use in error messages.
func TruncateValue(v any) any {
	switch val := v.(type) {
	case string:
		if len(val) > 50 {
			return val[:47] + "..."
		}
		return val
	case map[string]any:
		if len(val) > 3 {
			return "{...}"
		}
		return val
	case []any:
		if len(val) > 3 {
			return "[...]"
		}
		return val
	default:
		return v
	}
}

// truncateValue is an internal alias for TruncateValue.
func truncateValue(v any) any {
	return TruncateValue(v)
}
