package decoder

import (
	"encoding"
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

type unmarshalTextDecoder struct {
	typ        reflect.Type
	structName string
	fieldName  string
	// nullSetsZero is set for a value which null sets to its zero value ( see textUnmarshalerNullSetsZero ): null
	// leaves any other value as it is, and is not given to UnmarshalText.
	nullSetsZero bool
}

func newUnmarshalTextDecoder(typ reflect.Type, structName, fieldName string) *unmarshalTextDecoder {
	return &unmarshalTextDecoder{
		typ:          typ,
		structName:   structName,
		fieldName:    fieldName,
		nullSetsZero: textUnmarshalerNullSetsZero(typ.Elem().Kind()),
	}
}

var (
	nullbytes = []byte(`null`)
)

func (d *unmarshalTextDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	end, err := skipValue(buf, cursor, depth)
	if err != nil {
		return 0, err
	}
	src := buf[start:end]
	switch c := src[0]; {
	case c == 'n':
		if d.nullSetsZero {
			reflect.NewAt(d.typ.Elem(), p).Elem().SetZero()
		}
		return end, nil
	case c != '"':
		// a value of another kind than a string, which is a type error
		return ctx.textUnmarshalerKindError(start, depth, d.typ)
	}

	if s, ok := unquoteBytes(src); ok {
		src = s
	}
	v := *(*any)(unsafe.Pointer(&emptyInterface{
		typ: runtime.TypePtr(d.typ),
		ptr: *(*unsafe.Pointer)(unsafe.Pointer(&p)),
	}))
	if err := v.(encoding.TextUnmarshaler).UnmarshalText(src); err != nil {
		return ctx.methodError(cursor, end, err, d.structName, d.fieldName)
	}
	return end, nil
}

func (d *unmarshalTextDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: unmarshal text decoder does not support decode path")
}

func unquoteBytes(s []byte) (t []byte, ok bool) { //nolint: nonamedreturns
	length := len(s)
	if length < 2 || s[0] != '"' || s[length-1] != '"' {
		return
	}
	s = s[1 : length-1]
	length -= 2

	// Check for unusual characters. If there are none,
	// then no unquoting is needed, so return a slice of the
	// original bytes.
	r := 0
	for r < length {
		c := s[r]
		if c == '\\' || c == '"' || c < ' ' {
			break
		}
		if c < utf8.RuneSelf {
			r++
			continue
		}
		rr, size := utf8.DecodeRune(s[r:])
		if rr == utf8.RuneError && size == 1 {
			break
		}
		r += size
	}
	if r == length {
		return s, true
	}

	b := make([]byte, length+2*utf8.UTFMax)
	w := copy(b, s[0:r])
	for r < length {
		// Out of room? Can only happen if s is full of
		// malformed UTF-8 and we're replacing each
		// byte with RuneError.
		if w >= len(b)-2*utf8.UTFMax {
			nb := make([]byte, (len(b)+utf8.UTFMax)*2)
			copy(nb, b[0:w])
			b = nb
		}
		switch c := s[r]; {
		case c == '\\':
			r++
			if r >= length {
				return
			}
			switch s[r] {
			default:
				return
			case '"', '\\', '/', '\'':
				b[w] = s[r]
				r++
				w++
			case 'b':
				b[w] = '\b'
				r++
				w++
			case 'f':
				b[w] = '\f'
				r++
				w++
			case 'n':
				b[w] = '\n'
				r++
				w++
			case 'r':
				b[w] = '\r'
				r++
				w++
			case 't':
				b[w] = '\t'
				r++
				w++
			case 'u':
				r--
				rr := getu4(s[r:])
				if rr < 0 {
					return
				}
				r += 6
				if utf16.IsSurrogate(rr) {
					rr1 := getu4(s[r:])
					if dec := utf16.DecodeRune(rr, rr1); dec != unicode.ReplacementChar {
						// A valid pair; consume.
						r += 6
						w += utf8.EncodeRune(b[w:], dec)
						break
					}
					// Invalid surrogate; fall back to replacement rune.
					rr = unicode.ReplacementChar
				}
				w += utf8.EncodeRune(b[w:], rr)
			}

		// Quote, control characters are invalid.
		case c == '"', c < ' ':
			return

		// ASCII
		case c < utf8.RuneSelf:
			b[w] = c
			r++
			w++

		// Coerce to well-formed UTF-8.
		default:
			rr, size := utf8.DecodeRune(s[r:])
			r += size
			w += utf8.EncodeRune(b[w:], rr)
		}
	}
	return b[0:w], true
}

func getu4(s []byte) rune {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return -1
	}
	var r rune
	for _, c := range s[2:6] {
		switch {
		case '0' <= c && c <= '9':
			c = c - '0'
		case 'a' <= c && c <= 'f':
			c = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			c = c - 'A' + 10
		default:
			return -1
		}
		r = r*16 + rune(c)
	}
	return r
}
