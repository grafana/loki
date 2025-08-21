package executor

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// LogfmtTokenizer tokenizes logfmt formatted strings
type LogfmtTokenizer struct {
	input         string
	requestedKeys []string
	pos           int
	// For early stopping optimization
	requestedSet map[string]bool
	foundKeys    map[string]bool
	// Current token state
	key       string
	value     string
	wasQuoted bool    // Track if the value was quoted (needs unescaping)
	errors    []error // Collect all errors encountered
}

// NewLogfmtTokenizer creates a new tokenizer for the given input and requested keys
func NewLogfmtTokenizer(input string, requestedKeys []string) *LogfmtTokenizer {
	requestedSet := make(map[string]bool, len(requestedKeys))
	for _, key := range requestedKeys {
		requestedSet[key] = true
	}

	return &LogfmtTokenizer{
		input:         input,
		requestedKeys: requestedKeys,
		pos:           0,
		requestedSet:  requestedSet,
		foundKeys:     make(map[string]bool),
	}
}

// Next scans the next key-value pair from the input
// Returns false when scanning stops, either by reaching the end of input or when all requested keys are found (early stop optimization)
// When specific keys are requested, only returns those keys
func (t *LogfmtTokenizer) Next() bool {
	for {
		// Early stop optimization: if we have requested keys and found them all, stop
		if len(t.requestedSet) > 0 && len(t.foundKeys) == len(t.requestedSet) {
			return false
		}

		// Clear previous state
		t.key, t.value = "", ""
		t.wasQuoted = false

		// Skip whitespace
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t') {
			t.pos++
		}

		// Check if we've reached the end
		if t.pos >= len(t.input) {
			return false
		}

		// Find the key - scan until we hit '=', whitespace, or end of input
		keyStart := t.pos
		for t.pos < len(t.input) && t.input[t.pos] != '=' && t.input[t.pos] != ' ' && t.input[t.pos] != '\t' {
			t.pos++
		}

		// If we didn't move, something's wrong
		if t.pos == keyStart {
			return false
		}

		t.key = t.input[keyStart:t.pos]

		// Check for quotes in key (not allowed in logfmt)
		if strings.ContainsAny(t.key, `"'`) {
			// Only report error if: no filter (get all) OR key is requested
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// v1 format: "logfmt syntax error at pos X : unexpected '"'" or "invalid key"
				if strings.ContainsRune(t.key, '"') {
					t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : unexpected '\"'", keyStart))
				} else if strings.ContainsRune(t.key, '\'') {
					t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : unexpected '''", keyStart))
				} else {
					t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : invalid key", keyStart))
				}
			}
			// Skip to next whitespace
			for t.pos < len(t.input) && t.input[t.pos] != ' ' && t.input[t.pos] != '\t' {
				t.pos++
			}
			continue // Try next token
		}

		// Validate UTF-8 in key
		if !utf8.ValidString(t.key) {
			// Only report error if: no filter (get all) OR key is requested
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// v1 format: "logfmt syntax error at pos %d : invalid key"
				t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : invalid key", keyStart))
			}
			t.key = "" // Clear invalid key
			// Skip to next whitespace
			for t.pos < len(t.input) && t.input[t.pos] != ' ' && t.input[t.pos] != '\t' {
				t.pos++
			}
			continue // Try next token
		}

		// Check if this is a bare key (no '=' following)
		if t.pos >= len(t.input) || t.input[t.pos] == ' ' || t.input[t.pos] == '\t' {
			// Bare key - has no value
			t.value = ""

			// Check if this is a requested key (or no filter)
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// Track found keys for early stopping (if we have requested keys)
				if len(t.requestedSet) > 0 {
					t.foundKeys[t.key] = true
				}
				return true
			}
			continue // Skip non-requested bare keys
		}

		// Skip the '='
		t.pos++

		// Check for double equals (malformed input)
		if t.pos < len(t.input) && t.input[t.pos] == '=' {
			// Only report error if: no filter (get all) OR key is requested
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// v1 format: "logfmt syntax error at pos %d : unexpected '='"
				t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : unexpected '='", t.pos))
			}
			// Skip the extra '=' and continue to next token
			t.pos++
			// Skip to next whitespace to move past this malformed token
			for t.pos < len(t.input) && t.input[t.pos] != ' ' && t.input[t.pos] != '\t' {
				t.pos++
			}

			// Check if this is a requested key (or no filter)
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// Track found keys for early stopping (if we have requested keys)
				if len(t.requestedSet) > 0 {
					t.foundKeys[t.key] = true
				}
				return true // Return this error token if it's requested
			}
			continue // Skip non-requested keys
		}

		// Parse the value (quoted or unquoted)
		if t.pos < len(t.input) && (t.input[t.pos] == '"' || t.input[t.pos] == '\'') {
			// Quoted value - remember which quote type
			quoteChar := t.input[t.pos]
			quoteStart := t.pos
			t.pos++ // Skip opening quote
			valueStart := t.pos

			// Find closing quote of the same type
			foundClosingQuote := false
			for t.pos < len(t.input) {
				if t.input[t.pos] == quoteChar {
					// Check if it's escaped
					if t.pos > 0 && t.input[t.pos-1] == '\\' {
						t.pos++
						continue
					}
					// Found unescaped closing quote
					t.value = t.input[valueStart:t.pos]
					t.wasQuoted = true
					t.pos++ // Skip closing quote
					foundClosingQuote = true
					break
				}
				t.pos++
			}

			// No closing quote found - use what we have and report error
			if !foundClosingQuote {
				t.value = t.input[valueStart:]
				t.wasQuoted = true
				// v1 format: "logfmt syntax error at pos %d : unterminated quoted value"
				t.errors = append(t.errors, fmt.Errorf("logfmt syntax error at pos %d : unterminated quoted value", quoteStart))
			}

			// For quoted values, check UTF-8 in the raw value
			if t.wasQuoted && !utf8.ValidString(t.value) {
				// Only report error if: no filter (get all) OR key is requested
				if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
					// v1 doesn't have a specific format for UTF-8 in values, using generic
					t.errors = append(t.errors, fmt.Errorf("logfmt syntax error : invalid UTF-8 in value for key '%s'", t.key))
				}
				t.value = "" // Clear invalid value
			}
		} else {
			// Unquoted value
			valueStart := t.pos
			for t.pos < len(t.input) && t.input[t.pos] != ' ' && t.input[t.pos] != '\t' {
				t.pos++
			}
			t.value = t.input[valueStart:t.pos]
		}

		// Validate UTF-8 in value (unless it was quoted - we'll validate after unescaping)
		if !t.wasQuoted && !utf8.ValidString(t.value) {
			// Only report error if: no filter (get all) OR key is requested
			if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
				// v1 doesn't have a specific format for UTF-8 in values, using generic
				t.errors = append(t.errors, fmt.Errorf("logfmt syntax error : invalid UTF-8 in value for key '%s'", t.key))
			}
			t.value = "" // Clear invalid value
		}

		// Check if this is a requested key (or no filter)
		if len(t.requestedSet) == 0 || t.requestedSet[t.key] {
			// Track found keys for early stopping (if we have requested keys)
			if len(t.requestedSet) > 0 {
				t.foundKeys[t.key] = true
			}
			return true
		}
		// Continue to next token if this key wasn't requested
	}
}

// Key returns the key from the last call to Next
func (t *LogfmtTokenizer) Key() string {
	return t.key
}

// Value returns the value from the last call to Next
// If the value was quoted, escape sequences are processed
func (t *LogfmtTokenizer) Value() string {
	if t.wasQuoted {
		unescaped := unescapeValue(t.value)
		// Validate UTF-8 after unescaping
		if !utf8.ValidString(unescaped) {
			// We can't add to errors here since Value() is called after Next()
			// The error was likely already reported during parsing
			return ""
		}
		return unescaped
	}
	return t.value
}

// Err returns all errors encountered during tokenization, joined together
func (t *LogfmtTokenizer) Err() error {
	if len(t.errors) == 0 {
		return nil
	}
	return errors.Join(t.errors...)
}

// unescapeValue processes escape sequences in a quoted value
func unescapeValue(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s // Fast path: no escapes
	}

	var result strings.Builder
	result.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				result.WriteByte('\n')
				i++ // Skip the escaped character
			case 't':
				result.WriteByte('\t')
				i++
			case 'r':
				result.WriteByte('\r')
				i++
			case '\\':
				result.WriteByte('\\')
				i++
			case '"':
				result.WriteByte('"')
				i++
			case '\'':
				result.WriteByte('\'')
				i++
			default:
				// Unknown escape sequence, keep the backslash
				result.WriteByte('\\')
			}
		} else {
			result.WriteByte(s[i])
		}
	}

	return result.String()
}

// TokenizeLogfmt tokenizes logfmt input with smart early-stop and last-wins semantics
// It stops as soon as all requested keys are found, applying last-wins for any duplicates
// encountered before finding all keys. The tokenizer filters to only return requested keys.
func TokenizeLogfmt(input string, requestedKeys []string) (map[string]string, error) {
	result := make(map[string]string)
	tokenizer := NewLogfmtTokenizer(input, requestedKeys)

	// The tokenizer handles filtering and early stopping internally
	for tokenizer.Next() {
		// Last-wins semantics for duplicates
		result[tokenizer.Key()] = tokenizer.Value()
	}

	return result, tokenizer.Err()
}
