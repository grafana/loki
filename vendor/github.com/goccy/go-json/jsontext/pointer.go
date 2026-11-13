package jsontext

import (
	"iter"
	"strings"
	"unicode/utf8"
)

// Pointer is a JSON Pointer (RFC 6901), which references a JSON value relative to the root of the top-level
// JSON value.
//
// It is a list of tokens, each after a slash: a JSON object name, or the index of a JSON array element as a
// base-10 integer. An array index and an object name which is an integer can't be told apart without the
// structure of the value which the pointer refers to.
//
// A value has exactly one pointer, so two pointers are equal if and only if they point to the same value.
type Pointer string

// IsValid reports whether p is a valid JSON Pointer of RFC 6901. The concatenation of two valid pointers is a
// valid pointer.
func (p Pointer) IsValid() bool {
	if p != "" && p[0] != '/' {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' && (i+1 == len(p) || p[i+1] != '0' && p[i+1] != '1') {
			return false
		}
	}
	return utf8.ValidString(string(p))
}

// Contains reports whether the JSON value which p points to is the one which pc points to or contains it.
func (p Pointer) Contains(pc Pointer) bool {
	return strings.HasPrefix(string(pc), string(p)) && (len(pc) == len(p) || pc[len(p)] == '/')
}

// Parent returns p without its last token. The parent of the empty pointer is the empty pointer.
func (p Pointer) Parent() Pointer {
	return p[:max(strings.LastIndexByte(string(p), '/'), 0)]
}

// LastToken returns the last token of p. The last token of the empty pointer is the empty string.
func (p Pointer) LastToken() string {
	last := p[max(strings.LastIndexByte(string(p), '/'), 0):]
	return unescapePointerToken(strings.TrimPrefix(string(last), "/"))
}

// AppendToken appends the token tok to p and returns the pointer.
func (p Pointer) AppendToken(tok string) Pointer {
	if !utf8.ValidString(tok) {
		tok = string(appendValidUTF8(nil, tok))
	}
	return Pointer(appendPointerToken([]byte(p), tok))
}

// appendValidUTF8 appends s, whose each byte of invalid UTF-8 is U+FFFD.
func appendValidUTF8[Bytes ~[]byte | ~string](dst []byte, s Bytes) []byte {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(string(s[i:min(i+utf8.UTFMax, len(s))]))
		if r == utf8.RuneError && n == 1 {
			dst = append(dst, "�"...)
		} else {
			dst = append(dst, s[i:i+n]...)
		}
		i += n
	}
	return dst
}

// Tokens returns an iterator over the tokens of p, from the first to the last.
func (p Pointer) Tokens() iter.Seq[string] {
	return func(yield func(string) bool) {
		for s := string(p); s != ""; {
			s = strings.TrimPrefix(s, "/")
			i := strings.IndexByte(s, '/')
			if i < 0 {
				i = len(s)
			}
			if !yield(unescapePointerToken(s[:i])) {
				return
			}
			s = s[i:]
		}
	}
}

// appendPointerToken appends a slash and the token, whose '~' and '/' are escaped as "~0" and "~1".
func appendPointerToken[Bytes ~[]byte | ~string](b []byte, tok Bytes) []byte {
	b = append(b, '/')
	for i := 0; i < len(tok); i++ {
		switch tok[i] {
		case '~':
			b = append(b, "~0"...)
		case '/':
			b = append(b, "~1"...)
		default:
			b = append(b, tok[i])
		}
	}
	return b
}

func unescapePointerToken(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	return strings.NewReplacer("~1", "/", "~0", "~").Replace(s)
}
