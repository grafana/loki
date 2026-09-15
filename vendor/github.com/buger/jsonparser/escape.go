// SYS-REQ-014, SYS-REQ-060, SYS-REQ-061, SYS-REQ-062, SYS-REQ-063: string escape and Unicode handling
package jsonparser

import (
	"bytes"
	"unicode/utf8"
)

// JSON Unicode stuff: see https://tools.ietf.org/html/rfc7159#section-7

const supplementalPlanesOffset = 0x10000
const highSurrogateOffset = 0xD800
const lowSurrogateOffset = 0xDC00

const basicMultilingualPlaneReservedOffset = 0xDFFF
const basicMultilingualPlaneOffset = 0xFFFF

// NOTE: combineUTF16Surrogates is blocked by an unsupported `<<` shift op
// in the translator (Phase T.* gap, beyond #1/#2/#5). Even though Fix #6
// resolves the package-const references in this body, the shift remains
// untranslated, so no lemma is attached here.
func combineUTF16Surrogates(high, low rune) rune {
	return supplementalPlanesOffset + (high-highSurrogateOffset)<<10 + (low - lowSurrogateOffset)
}

const badHex = -1

//	reqproof:lemma h2I_range func(c byte) bool {
//	  r := h2I(c)
//	  return r == -1 || (r >= 0 && r <= 15)
//	}
//
//	reqproof:lemma h2I_decimal_digit func(c byte) bool {
//	  if c < '0' || c > '9' { return true }
//	  r := h2I(c)
//	  return r >= 0 && r <= 9
//	}
//
//	reqproof:lemma h2I_uppercase_hex func(c byte) bool {
//	  if c < 'A' || c > 'F' { return true }
//	  r := h2I(c)
//	  return r >= 10 && r <= 15
//	}
//
//	reqproof:lemma h2I_lowercase_hex func(c byte) bool {
//	  if c < 'a' || c > 'f' { return true }
//	  r := h2I(c)
//	  return r >= 10 && r <= 15
//	}
//
//	reqproof:lemma h2I_nondigit_is_badhex func(c byte) bool {
//	  if c >= '0' && c <= '9' { return true }
//	  if c >= 'A' && c <= 'F' { return true }
//	  if c >= 'a' && c <= 'f' { return true }
//	  return h2I(c) == badHex
//	}
//
//	reqproof:lemma h2I_nonneg_implies_le_15 func(c byte) bool {
//	  r := h2I(c)
//	  if r >= 0 {
//	    return r <= 15
//	  }
//	  return true
//	}
func h2I(c byte) int {
	if c >= 48 && c <= 57 { // '0'..'9'
		return int(c - 48)
	}
	if c >= 65 && c <= 70 { // 'A'..'F'
		return int(c-65) + 10
	}
	if c >= 97 && c <= 102 { // 'a'..'f'
		return int(c-97) + 10
	}
	return badHex
}

// decodeSingleUnicodeEscape decodes a single \uXXXX escape sequence. The prefix \u is assumed to be present and
// is not checked.
// In JSON, these escapes can either come alone or as part of "UTF16 surrogate pairs" that must be handled together.
// This function only handles one; decodeUnicodeEscape handles this more complex case.
func decodeSingleUnicodeEscape(in []byte) (rune, bool) {
	// We need at least 6 characters total
	if len(in) < 6 {
		return utf8.RuneError, false
	}

	// Convert hex to decimal
	h1, h2, h3, h4 := h2I(in[2]), h2I(in[3]), h2I(in[4]), h2I(in[5])
	if h1 == badHex || h2 == badHex || h3 == badHex || h4 == badHex {
		return utf8.RuneError, false
	}

	// Compose the hex digits
	return rune(h1<<12 + h2<<8 + h3<<4 + h4), true
}

// isUTF16EncodedRune checks if a rune is in the range for non-BMP characters,
// which is used to describe UTF16 chars.
// Source: https://en.wikipedia.org/wiki/Plane_(Unicode)#Basic_Multilingual_Plane
//
//	reqproof:lemma isUTF16EncodedRune_low_excluded func(r rune) bool {
//	  return !(r < 0xD800) || !isUTF16EncodedRune(r)
//	}
//
//	reqproof:lemma isUTF16EncodedRune_const_high_bound func(r rune) bool {
//	  // Fix #6: package-level const highSurrogateOffset (= 0xD800) now
//	  // resolves at translation time. Below the high surrogate offset
//	  // means definitely outside the UTF-16 surrogate range.
//	  return !(r < highSurrogateOffset) || !isUTF16EncodedRune(r)
//	}
//
//	reqproof:lemma isUTF16EncodedRune_const_bmp_bound func(r rune) bool {
//	  // Fix #6: package-level const basicMultilingualPlaneReservedOffset (= 0xDFFF).
//	  // Above the BMP-reserved offset means outside the UTF-16 surrogate range.
//	  return !(r > basicMultilingualPlaneReservedOffset) || !isUTF16EncodedRune(r)
//	}
func isUTF16EncodedRune(r rune) bool {
	return 0xD800 <= r && r <= 0xDFFF
}

// SYS-REQ-115
func decodeUnicodeEscape(in []byte) (rune, int) {
	if r, ok := decodeSingleUnicodeEscape(in); !ok {
		// Invalid Unicode escape
		return utf8.RuneError, -1
	} else if !isUTF16EncodedRune(r) {
		// Valid Unicode escape in Basic Multilingual Plane.
		// Note: a single \uXXXX escape produces r in [0, 0xFFFF], so r is always
		// within the BMP. The former r <= basicMultilingualPlaneOffset guard was
		// tautological and has been removed — the real discriminator is whether r
		// falls in the UTF-16 surrogate range.
		return r, 6
	} else if r >= lowSurrogateOffset {
		// Lone low surrogate (0xDC00-0xDFFF) with no preceding high surrogate.
		// Per RFC 8259/WHATWG a lone surrogate in a JSON string is malformed;
		// match encoding/json by substituting U+FFFD and consuming only the 6
		// bytes of this escape.
		return utf8.RuneError, 6
	} else if len(in) < 8 || in[6] != '\\' || in[7] != 'u' {
		// Lone high surrogate (0xD800-0xDBFF): the high-surrogate escape is not
		// followed by a "\u" low-surrogate escape. decodeSingleUnicodeEscape
		// assumes the \u prefix and reads hex at fixed offsets, so without this
		// guard it would misread whatever bytes follow (e.g. the literal "A7FA"
		// after "\uDB29") as a low surrogate and synthesize a bogus code point
		// (DEFECT-260727-SNGT). Substitute U+FFFD and consume only the 6 bytes
		// of the high surrogate, matching encoding/json.
		return utf8.RuneError, 6
	} else if r2, ok := decodeSingleUnicodeEscape(in[6:]); !ok {
		// A "\u" follows the high surrogate but the low-surrogate escape is
		// itself malformed (truncated / bad hex) — the whole escape is broken.
		return utf8.RuneError, -1
	} else if r2 < lowSurrogateOffset || r2 > basicMultilingualPlaneReservedOffset {
		// The following "\uXXXX" is not a valid low surrogate (0xDC00-0xDFFF):
		// e.g. a BMP codepoint or another high surrogate. Treat the first escape
		// as a lone high surrogate → U+FFFD, consuming 6 bytes; the following
		// escape is reprocessed by the caller.
		return utf8.RuneError, 6
	} else {
		// Valid UTF16 surrogate pair
		return combineUTF16Surrogates(r, r2), 12
	}
}

// backslashCharEscapeTable: when '\X' is found for some byte X, it is to be replaced with backslashCharEscapeTable[X]
var backslashCharEscapeTable = [...]byte{
	'"':  '"',
	'\\': '\\',
	'/':  '/',
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
}

// unescapeToUTF8 unescapes the single escape sequence starting at 'in' into 'out' and returns
// how many characters were consumed from 'in' and emitted into 'out'.
// If a valid escape sequence does not appear as a prefix of 'in', (-1, -1) to signal the error.
func unescapeToUTF8(in, out []byte) (inLen int, outLen int) {
	return unescapeToUTF8Config(DefaultConfig, in, out)
}

// SYS-REQ-115
func unescapeToUTF8Config(config Config, in, out []byte) (inLen int, outLen int) {
	if len(in) < 2 || in[0] != '\\' {
		// Invalid escape due to insufficient characters for any escape or no initial backslash
		return -1, -1
	}

	// https://tools.ietf.org/html/rfc7159#section-7
	switch e := in[1]; e {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		// Valid basic 2-character escapes (use lookup table)
		out[0] = backslashCharEscapeTable[e]
		return 2, 1
	case '\'':
		if config.AllowSingleQuotes {
			out[0] = e
			return 2, 1
		}
	case 'u':
		// Unicode escape
		if r, inLen := decodeUnicodeEscape(in); inLen == -1 {
			// Invalid Unicode escape
			return -1, -1
		} else {
			// Valid Unicode escape; re-encode as UTF8
			outLen := utf8.EncodeRune(out, r)
			return inLen, outLen
		}
	}

	if config.AllowUnknownEscapes {
		// Lenient mode treats the escaped byte as a literal and discards the
		// escape marker. A trailing '\' and malformed \u escape remain errors:
		// they are truncated/invalid encodings, not unknown escape names.
		out[0] = in[1]
		return 2, 1
	}

	return -1, -1
}

const lowerHex = "0123456789abcdef"

// Escape returns in as a JSON string literal, including the surrounding
// quotation marks.
// SYS-REQ-014
func Escape(in string) []byte {
	var stackbuf [unescapeStackBufSize]byte
	out := stackbuf[:0]
	out = append(out, '"')

	start := 0
	for i := 0; i < len(in); i++ {
		c := in[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}

		out = append(out, in[start:i]...)
		switch c {
		case '"', '\\':
			out = append(out, '\\', c)
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, '\\', 'u', '0', '0', lowerHex[c>>4], lowerHex[c&0x0f])
		}
		start = i + 1
	}

	out = append(out, in[start:]...)
	return append(out, '"')
}

// SetString replaces the value at keys with val encoded as a JSON string.
// SYS-REQ-009
func SetString(data []byte, val string, keys ...string) ([]byte, error) {
	return Set(data, Escape(val), keys...)
}

// unescape unescapes the string contained in 'in' and returns it as a slice.
// If 'in' contains no escaped characters:
//
//	Returns 'in'.
//
// Else, if 'out' is of sufficient capacity (guaranteed if cap(out) >= len(in)):
//
//	'out' is used to build the unescaped string and is returned with no extra allocation
//
// Else:
//
//	A new slice is allocated and returned.
func Unescape(in, out []byte) ([]byte, error) {
	return unescapeConfig(DefaultConfig, in, out)
}

// SYS-REQ-115
func unescapeConfig(config Config, in, out []byte) ([]byte, error) {
	firstBackslash := bytes.IndexByte(in, '\\')
	if firstBackslash == -1 {
		return in, nil
	}

	// Get a buffer of sufficient size (allocate if needed)
	if cap(out) < len(in) {
		out = make([]byte, len(in))
	} else {
		out = out[0:len(in)]
	}

	// Copy the first sequence of unescaped bytes to the output and obtain a buffer pointer (subslice)
	copy(out, in[:firstBackslash])
	in = in[firstBackslash:]
	buf := out[firstBackslash:]

	// The loop always exits via break: either on error (MalformedStringEscapeError)
	// or after copying the final non-escaped tail. The former `for len(in) > 0`
	// guard was structurally always true on re-entry since the else branch always
	// leaves at least the backslash character in `in`.
	for {
		// Unescape the next escaped character
		inLen, bufLen := unescapeToUTF8Config(config, in, buf)
		if inLen == -1 {
			return nil, MalformedStringEscapeError
		}

		in = in[inLen:]
		buf = buf[bufLen:]

		// Copy everything up until the next backslash
		nextBackslash := bytes.IndexByte(in, '\\')
		if nextBackslash == -1 {
			copy(buf, in)
			buf = buf[len(in):]
			break
		} else {
			copy(buf, in[:nextBackslash])
			buf = buf[nextBackslash:]
			in = in[nextBackslash:]
		}
	}

	// Trim the out buffer to the amount that was actually emitted
	return out[:len(out)-len(buf)], nil
}
