package jsonlite

// Valid reports whether json is a valid JSON string.
// This is similar to encoding/json.Valid but uses the jsonlite tokenizer
// for efficient zero-allocation validation.
func Valid(json string) bool {
	tok := Tokenize(json)
	if !valid(tok) {
		return false
	}
	// Ensure no trailing content after root value
	_, ok := tok.Next()
	return !ok
}

// valid validates a single JSON value and returns true if valid.
func valid(tok *Tokenizer) bool {
	token, ok := tok.Next()
	if !ok {
		return false
	}
	return validToken(tok, token)
}

// validToken validates a token and any nested structure it may contain.
func validToken(tok *Tokenizer, token string) bool {
	switch token[0] {
	case 'n':
		return token == "null"
	case 't':
		return token == "true"
	case 'f':
		return token == "false"
	case '"':
		return validString(token)
	case '[':
		return validArray(tok)
	case '{':
		return validObject(tok)
	default:
		return validNumber(token)
	}
}

// validString checks if a token is a valid JSON string.
// The token must start and end with quotes and contain valid escape sequences.
func validString(s string) bool {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return false
	}
	// Check for valid escape sequences and no unescaped control characters
	content := s[1 : len(s)-1]

	// Fast path: use SIMD-like scanning from unquote.go to check if we need
	// detailed validation. If no backslashes or control chars, string is valid.
	if !escaped(content) {
		return true
	}

	// Slow path: validate escape sequences
	return validStringEscapes(content)
}

// validStringEscapes validates escape sequences in a string that contains
// at least one backslash or control character.
func validStringEscapes(content string) bool {
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c < 0x20 {
			// Control characters must be escaped
			return false
		}
		if c == '\\' {
			i++
			if i >= len(content) {
				return false
			}
			switch content[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				// Valid single-character escape
			case 'u':
				// Unicode escape: must be followed by 4 hex digits
				if i+4 >= len(content) {
					return false
				}
				if !isHexDigit(content[i+1]) || !isHexDigit(content[i+2]) ||
					!isHexDigit(content[i+3]) || !isHexDigit(content[i+4]) {
					return false
				}
				i += 4
			default:
				return false
			}
		}
	}
	return true
}

// isHexDigit returns true if c is a valid hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// validArray validates a JSON array starting after the '[' token.
func validArray(tok *Tokenizer) bool {
	// Check for empty array
	token, ok := tok.Next()
	if !ok {
		return false
	}
	if token == "]" {
		return true
	}

	// Parse first element
	if !validToken(tok, token) {
		return false
	}

	// Parse remaining elements
	for {
		token, ok = tok.Next()
		if !ok {
			return false
		}
		if token == "]" {
			return true
		}
		if token != "," {
			return false
		}
		// Expect value after comma
		token, ok = tok.Next()
		if !ok {
			return false
		}
		if token == "]" {
			// Trailing comma is not valid JSON
			return false
		}
		if !validToken(tok, token) {
			return false
		}
	}
}

// validObject validates a JSON object starting after the '{' token.
func validObject(tok *Tokenizer) bool {
	for i := 0; ; i++ {
		token, ok := tok.Next()
		if !ok {
			return false
		}
		if token == "}" {
			return true // Empty object or end of object
		}
		if i > 0 {
			// After first field, expect comma then key
			if token != "," {
				return false
			}
			token, ok = tok.Next()
			if !ok {
				return false
			}
			if token == "}" {
				// Trailing comma is not valid JSON
				return false
			}
		}
		// Expect string key
		if len(token) == 0 || token[0] != '"' || !validString(token) {
			return false
		}
		// Expect colon
		token, ok = tok.Next()
		if !ok || token != ":" {
			return false
		}
		// Expect value
		if !valid(tok) {
			return false
		}
	}
}

// validNumber checks if a string is a valid JSON number.
// JSON numbers: -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
func validNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	i := 0

	// Optional minus sign
	if s[i] == '-' {
		i++
		if i >= len(s) {
			return false
		}
	}

	// Integer part
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return false
	}

	// Fractional part
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}

	// Exponent part
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i >= len(s) {
			return false
		}
		if s[i] == '+' || s[i] == '-' {
			i++
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}

	return i == len(s)
}
