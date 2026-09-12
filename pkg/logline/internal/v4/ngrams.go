package v4

import (
	"encoding/binary"
	"slices"

	"github.com/grafana/loki/pkg/push"
)

// v4 shares the v3 on-disk format byte-for-byte. The only difference is n-gram
// extraction: an all-digit gram is NumericNgramLength digits long instead of n,
// so that integer queries can narrow.
//
// A 6-digit all-digit gram lives in a 10^6 space, which is too small at
// production ingest rates: every value occurs in every document, so the writer's
// density filter stores it as a MatchesAll sentinel and an integer query
// decomposes into saturated grams whose AND narrows nothing. At 9 digits the
// space is 10^9, sparse enough for the term to carry real postings.

// Indexable punctuation characters that get normalized to '.' (period).
const normalizedPunct = '.'

// NumericNgramLength is the digit count of a numeric n-gram. See the package
// comment for why it is 9. Changing it changes the emitted n-gram set and so
// requires a new index version.
const NumericNgramLength = 9

// numericTag marks a packed numeric key. It must be a byte that can never
// appear in a text gram, so that numeric and text terms occupy disjoint regions
// of the shared term dictionary. Text grams only ever contain the transformed
// alphabet (space, '.', '0'-'9', 'A'-'Z'), so any byte below 0x20 is safe.
// 0x00 is deliberately left unused as an "empty key" sentinel.
const numericTag = 0x01

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

// ExtractFeatures performs the v4 feature extraction pipeline in a single pass:
//  1. Split into tokens at separators
//  2. Emit text n-grams of length n, skipping any whose bytes are all digits
//  3. Emit one packed numeric key per window of NumericNgramLength digits
//
// The input string is not modified. Duplicates are preserved in the output;
// callers can deduplicate when needed.
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

	// Two tight scans rather than one loop carrying token state: skip separators,
	// then run to the end of the token. This drops the per-byte "am I inside a
	// token" branch, and each scan has a single well-predicted exit condition.
	textLen := len(text)

	for i := 0; i < textLen; {
		for i < textLen && separatorTable[text[i]] {
			i++
		}
		start := i
		for i < textLen && !separatorTable[text[i]] {
			i++
		}
		if i-start >= n {
			ngrams = extractNgramsFromToken(n, text[start:i], ngrams)
		}
	}

	return ngrams
}

func extractNgramsFromToken(n int, token string, ngrams [][8]byte) [][8]byte {
	tokenLen := len(token)

	// Text grams: the v3 sliding window, minus the all-digit ones.
	//
	// Consecutive windows overlap by n-1 bytes, so rescanning each window from
	// scratch transforms and digit-tests every byte n times. The window is kept
	// incrementally instead: w holds the transformed bytes packed big-endian in
	// its top n bytes, and digits counts how many of them are ASCII digits, so
	// each byte of the token is transformed and tested exactly once.
	// longestRun is the longest digit run anywhere in the token. The window walk
	// below visits every byte exactly once, so tracking it there is free, and it
	// lets the numeric pass be skipped outright for the large majority of tokens
	// whose longest run is too short to produce a key.
	longestRun, run := 0, 0

	if tokenLen >= n {
		shift := uint(64 - 8*n)

		// Grow once for the token's worst case rather than testing capacity on
		// every emit. Writing through a pre-sized window turns each emit into a
		// store plus an index bump.
		base := len(ngrams)
		maxText := tokenLen - n + 1
		ngrams = slices.Grow(ngrams, maxText)
		dst := ngrams[base : base+maxText]
		out := 0

		var w uint64
		digits := 0
		for k := 0; k < n; k++ {
			c := token[k]
			w = w<<8 | uint64(transformTable[c])
			if isDigit(c) {
				digits++
				run++
				if run > longestRun {
					longestRun = run
				}
			} else {
				run = 0
			}
		}
		w <<= shift

		for j := 0; ; j++ {
			if digits < n {
				// An all-digit window is covered by the numeric grams below, at a
				// length that is actually selective. Emitting it here would only
				// add a saturated term.
				binary.BigEndian.PutUint64(dst[out][:], w)
				out++
			}

			next := j + n
			if next >= tokenLen {
				break
			}
			if isDigit(token[j]) {
				digits--
			}
			c := token[next]
			if isDigit(c) {
				digits++
				run++
				if run > longestRun {
					longestRun = run
				}
			} else {
				run = 0
			}
			w = w<<8 | uint64(transformTable[c])<<shift
		}

		ngrams = ngrams[:base+out]
	}

	// Numeric grams: one packed key per window of NumericNgramLength digits.
	if longestRun < NumericNgramLength {
		return ngrams
	}
	for j := 0; j < tokenLen; {
		if !isDigit(token[j]) {
			j++
			continue
		}
		runEnd := j
		for runEnd < tokenLen && isDigit(token[runEnd]) {
			runEnd++
		}
		for s := j; s+NumericNgramLength <= runEnd; s++ {
			ngrams = append(ngrams, packNumeric(token[s:s+NumericNgramLength]))
		}
		j = runEnd
	}

	return ngrams
}

// packNumeric encodes a run of decimal digits as a term key.
//
// Layout, in the 6 bytes the term dictionary and radix sort actually use:
//
//	byte 0   numericTag
//	byte 1-5 value, 40-bit big-endian
//
// Bytes 6-7 stay zero because radixSortByNgram only orders bytes 0-5 and
// assumes the rest are zero.
//
// Digits are packed as a base-10 integer rather than stored as ASCII: 9 ASCII
// digits would need 9 bytes and cannot fit, while the same value needs only 30
// bits. Digit strings of a fixed length map one-to-one onto integers, so leading
// zeros are preserved and two different runs can never share a key.
func packNumeric(digits string) [8]byte {
	var v uint64
	for i := 0; i < len(digits); i++ {
		v = v*10 + uint64(digits[i]-'0')
	}
	var key [8]byte
	key[0] = numericTag
	key[1] = byte(v >> 32)
	key[2] = byte(v >> 24)
	key[3] = byte(v >> 16)
	key[4] = byte(v >> 8)
	key[5] = byte(v)
	return key
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
