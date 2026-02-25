package xml2json

import (
	"bytes"
	"io"
	"unicode/utf8"
)

// An Encoder writes JSON objects to an output stream.
type Encoder struct {
	w               io.Writer
	err             error
	contentPrefix   string
	attributePrefix string
	tc              encoderTypeConverter
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer, plugins ...plugin) *Encoder {
	e := &Encoder{w: w, contentPrefix: contentPrefix, attributePrefix: attrPrefix}
	for _, p := range plugins {
		e = p.AddToEncoder(e)
	}
	return e
}

// Encode writes the JSON encoding of v to the stream
func (enc *Encoder) Encode(root *Node) error {
	if enc.err != nil {
		return enc.err
	}
	if root == nil {
		return nil
	}

	enc.err = enc.format(root, 0)

	// Terminate each value with a newline.
	// This makes the output look a little nicer
	// when debugging, and some kind of space
	// is required if the encoded value was a number,
	// so that the reader knows there aren't more
	// digits coming.
	enc.write("\n")

	return enc.err
}

func (enc *Encoder) format(n *Node, lvl int) error {
	if n.IsComplex() {
		enc.write("{")

		// Add data as an additional attibute (if any)
		if len(n.Data) > 0 {
			enc.write("\"")
			enc.write(enc.contentPrefix)
			enc.write("content")
			enc.write("\": ")
			enc.write(sanitiseString(n.Data))
			enc.write(", ")
		}

		i := 0
		tot := len(n.Children)
		for label, children := range n.Children {
			enc.write("\"")
			enc.write(label)
			enc.write("\": ")

			if n.ChildrenAlwaysAsArray || len(children) > 1 {
				// Array
				enc.write("[")
				for j, c := range children {
					enc.format(c, lvl+1)

					if j < len(children)-1 {
						enc.write(", ")
					}
				}
				enc.write("]")
			} else {
				// Map
				enc.format(children[0], lvl+1)
			}

			if i < tot-1 {
				enc.write(", ")
			}
			i++
		}

		enc.write("}")
	} else {
		s := sanitiseString(n.Data)
		if enc.tc == nil {
			// do nothing
		} else {
			s = enc.tc.Convert(s)
		}
		enc.write(s)

	}

	return nil
}

func (enc *Encoder) write(s string) {
	enc.w.Write([]byte(s))
}

// https://golang.org/src/encoding/json/encode.go?s=5584:5627#L788
var hex = "0123456789abcdef"

func sanitiseString(s string) string {
	var buf bytes.Buffer

	buf.WriteByte('"')

	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if 0x20 <= b && b != '\\' && b != '"' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			if start < i {
				buf.WriteString(s[start:i])
			}
			switch b {
			case '\\', '"':
				buf.WriteByte('\\')
				buf.WriteByte(b)
			case '\n':
				buf.WriteByte('\\')
				buf.WriteByte('n')
			case '\r':
				buf.WriteByte('\\')
				buf.WriteByte('r')
			case '\t':
				buf.WriteByte('\\')
				buf.WriteByte('t')
			default:
				// This encodes bytes < 0x20 except for \n and \r,
				// as well as <, > and &. The latter are escaped because they
				// can lead to security holes when user-controlled strings
				// are rendered into JSON and served to some browsers.
				buf.WriteString(`\u00`)
				buf.WriteByte(hex[b>>4])
				buf.WriteByte(hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			if start < i {
				buf.WriteString(s[start:i])
			}
			buf.WriteString(`\ufffd`)
			i += size
			start = i
			continue
		}
		// U+2028 is LINE SEPARATOR.
		// U+2029 is PARAGRAPH SEPARATOR.
		// They are both technically valid characters in JSON strings,
		// but don't work in JSONP, which has to be evaluated as JavaScript,
		// and can lead to security holes there. It is valid JSON to
		// escape them, so we do so unconditionally.
		// See http://timelessrepo.com/json-isnt-a-javascript-subset for discussion.
		if c == '\u2028' || c == '\u2029' {
			if start < i {
				buf.WriteString(s[start:i])
			}
			buf.WriteString(`\u202`)
			buf.WriteByte(hex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	if start < len(s) {
		buf.WriteString(s[start:])
	}

	buf.WriteByte('"')

	return buf.String()
}
