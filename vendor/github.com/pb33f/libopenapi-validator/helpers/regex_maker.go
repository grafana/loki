package helpers

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

var (
	baseDefaultPattern        = "[^/]*"
	DefaultPatternRegex       = regexp.MustCompile("^([^/]*)$")
	DefaultPatternRegexString = DefaultPatternRegex.String()
)

// GetRegexForPath returns a compiled regular expression for the given path template.
//
// This function takes a path template string `tpl` and generates a regular expression
// that matches the structure of the template. The template can include placeholders
// enclosed in braces `{}` with optional custom patterns.
//
// Placeholders in the template can be defined as:
//   - `{name}`: Matches any sequence of characters except '/'
//   - `{name:pattern}`: Matches the specified custom pattern
//
// The function ensures that the template is well-formed, with balanced and properly
// nested braces. If the template is invalid, an error is returned.
//
// Parameters:
//   - tpl: The path template string to convert into a regular expression.
//
// Returns:
//   - *regexp.Regexp: A compiled regular expression that matches the template.
//   - error: An error if the template is invalid or the regular expression cannot be compiled.
//
// Example:
//
//	regex, err := GetRegexForPath("/orders/{id:[0-9]+}/items/{itemId}")
//	// regex: ^/orders/([0-9]+)/items/([^/]+)$
//	// err: nil
func GetRegexForPath(tpl string) (*regexp.Regexp, error) {
	// Check if it is well-formed.
	idxs, errBraces := BraceIndices(tpl)
	if errBraces != nil {
		return nil, errBraces
	}

	// Backup the original.
	template := tpl

	pattern := bytes.NewBufferString("^")
	var end int

	for i := 0; i < len(idxs); i += 2 {

		// Set all values we are interested in.
		raw := tpl[end:idxs[i]]
		end = idxs[i+1]
		parts := strings.SplitN(tpl[idxs[i]+1:end-1], ":", 2)
		name := parts[0]
		patt := baseDefaultPattern
		if len(parts) == 2 {
			patt = parts[1]
		}

		// Name or pattern can't be empty.
		if name == "" || patt == "" {
			return nil, fmt.Errorf("missing name or pattern in %q", tpl[idxs[i]:end])
		}

		// Build the regexp pattern.
		_, err := fmt.Fprintf(pattern, "%s(%s)", regexp.QuoteMeta(raw), patt)
		if err != nil {
			return nil, err
		}

	}

	// Add the remaining.
	raw := tpl[end:]
	pattern.WriteString(regexp.QuoteMeta(raw))

	pattern.WriteByte('$')

	patternString := pattern.String()

	if patternString == DefaultPatternRegexString {
		return DefaultPatternRegex, nil
	}

	// Compile full regexp.
	reg, errCompile := regexp.Compile(patternString)
	if errCompile != nil {
		return nil, errCompile
	}

	// Check for capturing groups which used to work in older versions
	if reg.NumSubexp() != len(idxs)/2 {
		return nil, fmt.Errorf("route %s contains capture groups in its regexp. Only non-capturing groups are accepted: e.g. (?:pattern) instead of (pattern)", template)
	}

	// Done!
	return reg, nil
}

// BraceIndices returns the indices of the opening and closing braces in a string.
//
// It scans the input string `s` and identifies the positions of matching pairs
// of braces ('{' and '}'). The function ensures that the braces are balanced
// and properly nested.
//
// If the braces are unbalanced or improperly nested, an error is returned.
//
// Parameters:
//   - s: The input string to scan for braces.
//
// Returns:
//   - []int: A slice of integers where each pair of indices represents the
//     start and end positions of a matching pair of braces.
//   - error: An error if the braces are unbalanced or improperly nested.
//
// Example:
//
//	indices, err := BraceIndices("/orders/{id}/items/{itemId}")
//	// indices: [8, 12, 19, 26]
//	// err: nil
func BraceIndices(s string) ([]int, error) {
	var level, idx int
	var idxs []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			if level++; level == 1 {
				idx = i
			}
		case '}':
			if level--; level == 0 {
				idxs = append(idxs, idx, i+1)
			} else if level < 0 {
				return nil, fmt.Errorf("unbalanced braces in %q", s)
			}
		}
	}
	if level != 0 {
		return nil, fmt.Errorf("unbalanced braces in %q", s)
	}
	return idxs, nil
}
