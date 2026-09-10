package v3

import "github.com/grafana/loki/pkg/push"

// Indexable punctuation characters that get normalized to '.' (period).
// These are: . _ - / : @ % ?
const normalizedPunct = '.'

// transformTable maps each byte to its transformed value:
// - lowercase a-z -> uppercase A-Z
// - indexable punctuation (_-/:@%?) -> '.'
// - everything else unchanged
var transformTable [256]byte

// separatorTable marks which bytes are separators (true = separator).
// Separators are: non-space whitespace, non-indexable punctuation, non-ASCII.
var separatorTable [256]bool

func init() {
	for i := range transformTable {
		transformTable[i] = byte(i)
	}
	for c := byte('a'); c <= 'z'; c++ {
		transformTable[c] = c - ('a' - 'A')
	}
	for _, c := range []byte("_-/:@%?") {
		transformTable[c] = normalizedPunct
	}
	transformTable['.'] = '.'

	for i := range separatorTable {
		separatorTable[i] = true
	}
	for c := byte('A'); c <= 'Z'; c++ {
		separatorTable[c] = false
	}
	for c := byte('a'); c <= 'z'; c++ {
		separatorTable[c] = false
	}
	for c := byte('0'); c <= '9'; c++ {
		separatorTable[c] = false
	}
	separatorTable[' '] = false
	for _, c := range []byte("._-/:@%?") {
		separatorTable[c] = false
	}
}

// ExtractFeatures performs the v3 feature extraction pipeline in a single pass:
// 1. Transform to uppercase
// 2. Normalize indexable punctuation to '.'
// 3. Split into tokens at separators
// 4. Extract n-grams from tokens that are >= n in length
//
// The input string is not modified. Duplicates are preserved in the output;
// callers can deduplicate when needed.
//
// v3 shares the v2 n-gram algorithm but additionally extracts from structured
// metadata and stream label values. Never modify the core tokenisation in a way
// that breaks already-written v3 indexes.
func ExtractFeatures(n int, line string, structuredMetadata push.LabelsAdapter, labelValues []string, ngrams [][8]byte) [][8]byte {
	ngrams = extractFromText(n, line, ngrams)
	for _, lbl := range structuredMetadata {
		ngrams = extractFromText(n, lbl.Value, ngrams)
	}
	for _, value := range labelValues {
		ngrams = extractFromText(n, value, ngrams)
	}
	return ngrams
}

func extractFromText(n int, text string, ngrams [][8]byte) [][8]byte {
	if n <= 0 || n > 8 || len(text) < n {
		return ngrams
	}

	tokenStart := -1
	textLen := len(text)

	for i := range textLen {
		if separatorTable[text[i]] {
			if tokenStart >= 0 {
				tokenLen := i - tokenStart
				if tokenLen >= n {
					ngrams = extractNgramsFromToken(n, text[tokenStart:i], ngrams)
				}
				tokenStart = -1
			}
			continue
		}

		if tokenStart < 0 {
			tokenStart = i
		}
	}

	if tokenStart >= 0 {
		tokenLen := textLen - tokenStart
		if tokenLen >= n {
			ngrams = extractNgramsFromToken(n, text[tokenStart:textLen], ngrams)
		}
	}

	return ngrams
}

func extractNgramsFromToken(n int, token string, ngrams [][8]byte) [][8]byte {
	tokenLen := len(token)
	for j := 0; j <= tokenLen-n; j++ {
		var key [8]byte
		for k := range n {
			key[k] = transformTable[token[j+k]]
		}
		ngrams = append(ngrams, key)
	}
	return ngrams
}
