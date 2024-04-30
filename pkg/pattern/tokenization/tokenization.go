package tokenization

import (
	"bytes"
	"unsafe"
)

const placeholderEndOfLine = "<...>"

// Outside of quoted strings, these are the delimiters between tokens. However,
// they are not going to be a part of the tokens themselves and will be replaced
// by spaces in the actual log template.
var delimiters = [256]bool{0: true, '\t': true, '\n': true, '\v': true, '\f': true, '\r': true, ' ': true}

type tokenizer struct {
	// Input
	rawLine   []byte
	maxTokens int

	// State and position iterators
	buf        []byte
	tpos       int
	tokenCount int
	maybeJSON  bool

	// Result values, the values in the `tokens` slice reference line and shouldn't
	// allocate new memory.
	line   string
	tokens []string

	tokeniseWholeQuotes bool
}

func (t *tokenizer) countOrSaveToken(endTokenPos, skip int) {
	if t.tokens != nil {
		// Intentionally written like this and not with append(), so this can
		// panic if we ever exceed the preallocated slice size, since that means
		// we have a nasty bug in handleNextToken() below.
		t.tokens[t.tokenCount] = t.line[t.tpos:endTokenPos]
	}
	t.tokenCount++
	t.tpos = endTokenPos + skip
}

func (t *tokenizer) handleNextToken() bool {
	escaped := false
	var c, curQuoteChar byte
	curQuotePos := -1

	lineLen := len(t.line)
	for p := t.tpos; p < lineLen; p++ {
		c = t.line[p]
		switch {

		// If the previous character was a backslash, we ignore the next
		// character, unless it was an non-token delimiter (space, tab, etc.)
		// outside of a quoted string.
		case escaped:
			if curQuotePos < 0 && delimiters[c] {
				t.countOrSaveToken(p, 1)
				return true
			} else {
				escaped = false
			}

		// If we weren't already escaped and we encounter a backslash, toggle
		// the escaped flag and ignore the current byte.
		case c == '\\':
			escaped = true

		// Non-ASCII / UTF8 / invalid character, consider it a part of the
		// current token, for lack of a better efficient option...
		case c > 127:
			// Intentionally blank, continues to the next char

		// If we are currently in a quoted part of the string, the current
		// character is also part of the current token. The only special case
		// here is if the current character is a matching quote, that means
		// we'll no longer be quoted.
		case t.tokeniseWholeQuotes && curQuotePos >= 0:
			if c == curQuoteChar { // end quote
				curQuotePos = -1
			}

		// If we encounter a qoute character and we were not already in a quoted
		// part of the line, mark that we are now in a quote from that type.
		case t.tokeniseWholeQuotes && (c == '"' || c == '\'' || c == '`'):
			curQuoteChar = c
			curQuotePos = p

		// If we encounter a delimiter outside of a quote, count or save the
		// token and skip the delimiter.
		case delimiters[c]:
			t.countOrSaveToken(p, 1)
			return true

		// Handle likely JSON object keys that have been serialized without
		// spaces. For example, something like this:
		//   `{"context":{"taskId":1},"message":"starting",...}`
		//
		// If the line starts or ends with curly braces, we consider it might be
		// a JSON log and try to detect the `":` part of the message that isn't
		// followed by a delimiter character. If we find that pattern, we
		// consider everything up to and including the `:` character as a
		// separate token.
		//
		// Similarly, we try to detect the `,"` pattern and also split a token
		// before the comma value. The `p > t.tpos` check is crucial here,
		// because it ensures that we're not at the start of a token, i.e. there
		// wasn't a delimiter right before the comma.
		case t.maybeJSON && p > t.tpos && (c == ':' || c == ',') && p+1 < lineLen:
			if c == ':' && t.line[p-1] == '"' && !delimiters[t.line[p+1]] {
				t.countOrSaveToken(p+1, 0)
				return true
			}
			if c == ',' && t.line[p+1] == '"' {
				t.countOrSaveToken(p, 0)
				return true
			}
		}

		// By default we do nothing, simply advance one character forward
		// because the current character was a part of the current token.
	}

	// We have an unterminated single quote at position `curQuotePos`. To handle
	// this edge case somewhat gracefully, we can emit everything up to that
	// unterminated quote and the quote itself as a single token, and continue
	// fairly normally from there.
	if curQuotePos > 0 {
		t.countOrSaveToken(curQuotePos+1, 0)
		return true
	}

	if t.tpos < len(t.line) {
		t.countOrSaveToken(len(t.line), 0)
		return true
	}

	return false
}

// This function is called twice! The first time it counts the tokens but
// doesn't save them. Afterwards we allocate the tokens return slice with
// exactly the correct capacity and we call it again, this time to save them.
func (t *tokenizer) process() {
	// We want to handle the end of the string as a single token, so we start
	// the loop from 1.
	for i := 1; i < t.maxTokens; i++ {
		if !t.handleNextToken() {
			break
		}
	}

	if t.tpos >= len(t.line) {
		return
	}

	// We have token count more than or equal to maxTokens, add one last token
	// containing placeholderEndOfLine to signal that.
	if t.tokens != nil {
		t.tokens[t.tokenCount] = placeholderEndOfLine
	}
	t.tokenCount++
	t.tpos += len(placeholderEndOfLine)
}

func (t *tokenizer) tokenize() []string {
	t.buf = Preprocess(t.rawLine)

	// We use unsafe to convert buf to a string without any new allocations.
	// This is safe because t.buf won't be used or referenced anywhere else
	// besides here from now on.
	t.line = unsafe.String(unsafe.SliceData(t.buf), len(t.buf))

	if len(t.line) >= 2 && (t.line[0] == '{' || t.line[len(t.line)-1] == '}') {
		t.maybeJSON = true
	}

	t.process()

	// If we have super long lines (more than twice the size we need to get the
	// maxTokens we want), copy just the part we need so the tokens don't hold a
	// reference to the original huge []byte slice.
	if t.tokenCount == t.maxTokens && t.tpos*2 < len(t.line) {
		t.line = string(t.buf[0 : t.tpos+1])
	}

	t.tokens = make([]string, t.tokenCount) // intentionally like this, see comment in countOrSaveToken()
	t.tokenCount = 0
	t.tpos = 0
	t.process()

	return t.tokens
}

func PreprocessAndTokenize(content []byte) []string {
	content = bytes.TrimSpace(content)

	t := tokenizer{rawLine: content, maxTokens: 100} // TODO: parametrize maxTokens

	return t.tokenize()
}
