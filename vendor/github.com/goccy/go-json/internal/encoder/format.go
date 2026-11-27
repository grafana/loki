package encoder

import (
	"encoding/binary"
	"math/bits"

	"github.com/goccy/go-json/internal/jsonnum"
)

// The formatting of valid JSON in one pass, compacted or indented: the input is the JSON followed by the nul
// byte, which ends it. A byte which the grammar doesn't have where it is stops the pass, and the error is then
// the one of jsontext ( see appendCompact ), which only an input which is not valid costs.

// maxFormatDepth is the nesting of the values which the pass formats: jsontext reports a deeper one.
const maxFormatDepth = 10000

// formatIndent is the indent of an indented pass: nil for a compact one.
type formatIndent struct {
	prefix string
	indent string
}

// appendNewline appends a new line and the indent of the depth.
func (f *formatIndent) appendNewline(dst []byte, depth int) []byte {
	dst = append(append(dst, '\n'), f.prefix...)
	for i := 0; i < depth; i++ {
		dst = append(dst, f.indent...)
	}
	return dst
}

// formatJSON appends src, valid JSON followed by the nul byte, to dst compacted, or indented by ind, with the
// characters for HTML escaped if escape is set, and returns the position after the white space after the value;
// or false if src is not valid JSON, and then dst is to be dropped.
func formatJSON(dst, src []byte, escape bool, ind *formatIndent) ([]byte, int64, bool) {
	dst, cursor, ok := formatValue(dst, src, skipFormatSpace(src, 0), escape, ind, 0)
	if !ok {
		return dst, 0, false
	}
	end := skipFormatSpace(src, cursor)
	if end != int64(len(src))-1 {
		return dst, 0, false
	}
	return dst, cursor, true
}

func skipFormatSpace(src []byte, cursor int64) int64 {
	for {
		switch src[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
			continue
		}
		return cursor
	}
}

func formatValue(dst, src []byte, cursor int64, escape bool, ind *formatIndent, depth int) ([]byte, int64, bool) {
	switch c := src[cursor]; c {
	case '{':
		return formatObject(dst, src, cursor, escape, ind, depth)
	case '[':
		return formatArray(dst, src, cursor, escape, ind, depth)
	case '"':
		return formatString(dst, src, cursor, escape)
	case 't':
		return formatLiteral(dst, src, cursor, "true")
	case 'f':
		return formatLiteral(dst, src, cursor, "false")
	case 'n':
		return formatLiteral(dst, src, cursor, "null")
	default:
		if c == '-' || c-'0' <= 9 {
			n, place := jsonnum.Scan(src[cursor : len(src)-1])
			if place != jsonnum.Valid {
				return dst, 0, false
			}
			end := cursor + int64(n)
			return append(dst, src[cursor:end]...), end, true
		}
		return dst, 0, false
	}
}

func formatLiteral(dst, src []byte, cursor int64, lit string) ([]byte, int64, bool) {
	end := cursor + int64(len(lit))
	if end > int64(len(src))-1 || string(src[cursor:end]) != lit {
		return dst, 0, false
	}
	return append(dst, lit...), end, true
}

func formatObject(dst, src []byte, cursor int64, escape bool, ind *formatIndent, depth int) ([]byte, int64, bool) {
	if depth++; depth > maxFormatDepth {
		return dst, 0, false
	}
	dst = append(dst, '{')
	cursor = skipFormatSpace(src, cursor+1)
	if src[cursor] == '}' {
		return append(dst, '}'), cursor + 1, true
	}
	var ok bool
	for {
		if ind != nil {
			dst = ind.appendNewline(dst, depth)
		}
		if src[cursor] != '"' {
			return dst, 0, false
		}
		if dst, cursor, ok = formatString(dst, src, cursor, escape); !ok {
			return dst, 0, false
		}
		if cursor = skipFormatSpace(src, cursor); src[cursor] != ':' {
			return dst, 0, false
		}
		dst = append(dst, ':')
		if ind != nil {
			dst = append(dst, ' ')
		}
		if dst, cursor, ok = formatValue(dst, src, skipFormatSpace(src, cursor+1), escape, ind, depth); !ok {
			return dst, 0, false
		}
		switch cursor = skipFormatSpace(src, cursor); src[cursor] {
		case ',':
			dst = append(dst, ',')
			cursor = skipFormatSpace(src, cursor+1)
		case '}':
			if ind != nil {
				dst = ind.appendNewline(dst, depth-1)
			}
			return append(dst, '}'), cursor + 1, true
		default:
			return dst, 0, false
		}
	}
}

func formatArray(dst, src []byte, cursor int64, escape bool, ind *formatIndent, depth int) ([]byte, int64, bool) {
	if depth++; depth > maxFormatDepth {
		return dst, 0, false
	}
	dst = append(dst, '[')
	cursor = skipFormatSpace(src, cursor+1)
	if src[cursor] == ']' {
		return append(dst, ']'), cursor + 1, true
	}
	var ok bool
	for {
		if ind != nil {
			dst = ind.appendNewline(dst, depth)
		}
		if dst, cursor, ok = formatValue(dst, src, cursor, escape, ind, depth); !ok {
			return dst, 0, false
		}
		switch cursor = skipFormatSpace(src, cursor); src[cursor] {
		case ',':
			dst = append(dst, ',')
			cursor = skipFormatSpace(src, cursor+1)
		case ']':
			if ind != nil {
				dst = ind.appendNewline(dst, depth-1)
			}
			return append(dst, ']'), cursor + 1, true
		default:
			return dst, 0, false
		}
	}
}

// formatString appends the string at cursor, whose escapes are validated and which has no control character.
// Its plain characters are skipped 8 at a time; invalid UTF-8 is kept as it is.
func formatString(dst, src []byte, cursor int64, escape bool) ([]byte, int64, bool) {
	start := cursor
	cursor++
	n := int64(len(src))
	for {
		if !escape {
			// the next quote, backslash or control character, by words
			for cursor+8 <= n {
				w := binary.LittleEndian.Uint64(src[cursor:])
				q, b := w^(lsb*'"'), w^(lsb*'\\')
				if m := ((q-lsb)&^q | (b-lsb)&^b | (w-lsb*0x20)&^w) & msb; m != 0 {
					cursor += int64(bits.TrailingZeros64(m) / 8)
					break
				}
				cursor += 8
			}
		}
		c := src[cursor]
		switch {
		case c == '"':
			cursor++
			return append(dst, src[start:cursor]...), cursor, true
		case c == '\\':
			end := escapeEnd(src[:n-1], int(cursor)+1)
			if end < 0 {
				return dst, 0, false
			}
			cursor = int64(end)
		case c < ' ':
			// a control character, or the nul byte which ends the input
			return dst, 0, false
		case escape && (c == '<' || c == '>' || c == '&'):
			dst = append(dst, src[start:cursor]...)
			dst = append(dst, '\\', 'u', '0', '0', "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0xf])
			cursor++
			start = cursor
		case escape && c == 0xe2 && cursor+2 < n-1 && src[cursor+1] == 0x80 && src[cursor+2]&^1 == 0xa8:
			// U+2028 and U+2029
			dst = append(dst, src[start:cursor]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', "0123456789abcdef"[src[cursor+2]&0xf])
			cursor += 3
			start = cursor
		default:
			cursor++
		}
	}
}
