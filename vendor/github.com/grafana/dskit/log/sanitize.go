package log

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DropUnsafeChars wraps v so that control and formatting characters are
// removed from its string representation when logged. This helps prevent
// log injection, terminal escape sequence injection, and other attacks
// that rely on special characters in untrusted input.
//
// Use it when logging values that may originate from users or other
// untrusted sources.
//
//	level.Info(logger).Log("user_agent", log.DropUnsafeChars(r.UserAgent()))
func DropUnsafeChars(v any) DroppedUnsafeChars {
	return DroppedUnsafeChars{v: v}
}

// DroppedUnsafeChars is the value returned by DropUnsafeChars. It
// implements fmt.Stringer and json.Marshaler for compatibility with
// both logfmt and JSON go-kit encoders.
type DroppedUnsafeChars struct {
	v any
}

// String returns the string representation of v with unsafe characters
// removed. Tab is preserved. If no unsafe characters are present, the
// original string is returned unchanged.
func (d DroppedUnsafeChars) String() string {
	s := render(d.v)
	if !needsSanitization(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		// An invalid UTF-8 byte (e.g. a lone C1 continuation byte such
		// as 0x85) decodes to RuneError with size 1. Drop it so that
		// malformed bytes cannot smuggle control characters past the
		// sanitiser.
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if isUnsafe(r) {
			i += size
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

// MarshalJSON encodes the sanitized value as a JSON string. Without this
// method, json.Marshal would encode the wrapper as an empty object
// because its fields are unexported.
//
// A nil value (a nil interface or a typed-nil pointer) is encoded as
// the JSON null literal so the output matches go-kit's behaviour.
func (d DroppedUnsafeChars) MarshalJSON() ([]byte, error) {
	if isNullValue(d.v) {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// MarshalText returns the sanitized value in text form. go-kit's logfmt
// encoder prefers encoding.TextMarshaler over fmt.Stringer.
//
// This ensures typed-nil errors are encoded as the unquoted `key=null`
// form that go-kit produces for the corresponding raw value, rather
// than passing through the Stringer path.
func (d DroppedUnsafeChars) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// EscapeUnsafeChars wraps v so that control and formatting characters
// are replaced with printable escape sequences in its string
// representation when logged.
//
// Use it for values that may originate from untrusted sources to help
// prevent log injection and terminal escape sequence injection while
// preserving a readable representation of the original value.
//
//	level.Info(logger).Log("user_agent", log.EscapeUnsafeChars(r.UserAgent()))
func EscapeUnsafeChars(v any) EscapedUnsafeChars {
	return EscapedUnsafeChars{v: v}
}

// EscapedUnsafeChars is the value returned by EscapeUnsafeChars. It
// implements fmt.Stringer and json.Marshaler for compatibility with
// both logfmt and JSON go-kit encoders.
type EscapedUnsafeChars struct {
	v any
}

// String returns the string representation of v with unsafe characters
// replaced by printable escape sequences. Tab is preserved. If no
// unsafe characters are present, the original string is returned
// unchanged.
func (e EscapedUnsafeChars) String() string {
	s := render(e.v)
	if !needsSanitization(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		// An invalid UTF-8 byte (e.g. a lone C1 continuation byte such
		// as 0x85) decodes to RuneError with size 1. Emit \xNN for the
		// raw byte so its value survives sanitisation for forensic use.
		if r == utf8.RuneError && size == 1 {
			writeEscape(&b, rune(s[i]))
			i++
			continue
		}
		if isUnsafe(r) {
			writeEscape(&b, r)
			i += size
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

// MarshalJSON serialises the sanitized form as a JSON string.
//
// A nil value (a nil interface or a typed-nil pointer) is encoded as
// the JSON null literal so the output matches go-kit's behaviour.
func (e EscapedUnsafeChars) MarshalJSON() ([]byte, error) {
	if isNullValue(e.v) {
		return []byte("null"), nil
	}
	return json.Marshal(e.String())
}

// MarshalText returns the sanitized form as bytes. See the rationale
// on DroppedUnsafeChars.MarshalText for why this is needed alongside
// String for correct logfmt encoding of typed-nil errors.
func (e EscapedUnsafeChars) MarshalText() ([]byte, error) {
	return []byte(e.String()), nil
}

// render returns the string representation of v, avoiding a
// fmt.Sprint allocation when v is already a string or an error.
//
// Nil values (a nil interface or a non-nil interface wrapping a nil
// concrete pointer) render as "null" to match what go-kit's encoders
// produce for the same input and to avoid calling Error() on a nil
// receiver.
func render(v any) string {
	if isNullValue(v) {
		return "null"
	}
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	}
	return fmt.Sprint(v)
}

// isNullValue reports whether v should be encoded as a null literal —
// either a nil interface (the dynamic type and value are both nil) or
// a non-nil interface wrapping a nil concrete pointer.
func isNullValue(v any) bool {
	if v == nil {
		return true
	}
	// Fast path: a string is a value type and cannot be a typed-nil
	// pointer, so we can return false without using reflection. Strings
	// are the most common values logged at call sites, making this a
	// worthwhile optimization.
	if _, ok := v.(string); ok {
		return false
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// isUnsafe reports whether r should be sanitized. It matches Unicode
// categories Cc (control characters), Cf (formatting characters,
// including bidi overrides), Zl (line separators), and Zp (paragraph
// separators).
//
// Tab is treated as safe because it has legitimate uses (for example,
// stack traces and tabular output) and is already escaped by logfmt and
// JSON encoders.
func isUnsafe(r rune) bool {
	if r == '\t' {
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// needsSanitization reports whether s contains any character the
// sanitizers would alter.
//
// Most log data is plain ASCII, so we optimize for that common case by
// scanning bytes first:
//
//   - printable ASCII characters (0x20-0x7E) and tab are considered safe
//   - all other ASCII control characters, including DEL, are unsafe
//   - non-ASCII bytes trigger a fallback to rune-level validation
//
// The rune-level path checks for Unicode control, formatting, line, and
// paragraph separator characters (for example U+2028 and U+2029) that
// could be used to manipulate log output.
//
// This avoids decoding every rune in ordinary ASCII strings while still
// correctly handling dangerous Unicode characters.
func needsSanitization(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\t':
			// Safe (matches the exception in isUnsafe).
		case c >= 0x20 && c < 0x7f:
			// Printable ASCII — safe.
		case c < 0x80:
			// ASCII C0 control or DEL — unsafe.
			return true
		default:
			// Non-ASCII: defer to rune-level check from here.
			remainder := s[i:]
			if !utf8.ValidString(remainder) {
				return true
			}
			return strings.ContainsFunc(remainder, isUnsafe)
		}
	}
	return false
}

// writeEscape appends a printable escape sequence for r to b, using
// \xNN, \uNNNN, or \UNNNNNNNN depending on the rune's value.
func writeEscape(b *strings.Builder, r rune) {
	switch {
	case r < 0x100:
		fmt.Fprintf(b, `\x%02x`, r)
	case r < 0x10000:
		fmt.Fprintf(b, `\u%04x`, r)
	default:
		fmt.Fprintf(b, `\U%08x`, r)
	}
}
