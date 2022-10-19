// +build !launchdarkly_easyjson

package jwriter

import (
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// This file defines the default implementation of the low-level JSON token writer. If the launchdarkly_easyjson
// build tag is enabled, we use the easyjson adapter in token_writer_easyjson.go instead. These have the same
// methods so the Writer code does not need to know which implementation we're using; however, we don't
// actually define an interface for these, because calling the methods through an interface would limit
// performance.

var (
	tokenNull  = []byte("null")  //nolint:gochecknoglobals
	tokenTrue  = []byte("true")  //nolint:gochecknoglobals
	tokenFalse = []byte("false") //nolint:gochecknoglobals
)

type tokenWriter struct {
	buf       streamableBuffer
	tempBytes [50]byte
}

func newTokenWriter() tokenWriter {
	return tokenWriter{}
}

func newStreamingTokenWriter(dest io.Writer, bufferSize int) tokenWriter {
	tw := tokenWriter{}
	tw.buf.Grow(bufferSize)
	tw.buf.SetStreamingWriter(dest, bufferSize)
	return tw
}

// Bytes returns the full encoded byte slice.
//
// If the buffer is in a failed state from a previous invalid operation, Bytes() returns any data written
// so far.
func (tw *tokenWriter) Bytes() []byte {
	return tw.buf.Bytes()
}

// Grow expands the internal buffer by the specified number of bytes. It is the same as calling Grow
// on a bytes.Buffer.
func (tw *tokenWriter) Grow(n int) {
	tw.buf.Grow(n)
}

// Flush writes any remaining in-memory output to the underlying Writer, if this is a streaming buffer
// created with newStreamingTokenWriter. It has no effect otherwise.
func (tw *tokenWriter) Flush() error {
	return tw.buf.Flush()
}

// Null writes a JSON null.
func (tw *tokenWriter) Null() error {
	tw.buf.Write(tokenNull)
	return tw.buf.GetWriterError()
}

// Bool writes a JSON boolean.
func (tw *tokenWriter) Bool(value bool) error {
	var out []byte
	if value {
		out = tokenTrue
	} else {
		out = tokenFalse
	}
	tw.buf.Write(out)
	return tw.buf.GetWriterError()
}

// Int writes an integer JSON number.
func (tw *tokenWriter) Int(value int) error {
	if value == 0 {
		tw.buf.WriteByte('0')
	} else {
		out := tw.tempBytes[0:0]
		out = strconv.AppendInt(out, int64(value), 10)
		tw.buf.Write(out)
	}
	return tw.buf.GetWriterError()
}

// Float64 writes a JSON number.
func (tw *tokenWriter) Float64(value float64) error {
	if value == 0 {
		tw.buf.WriteByte('0')
	} else {
		i := int(value)
		if float64(i) == value {
			return tw.Int(i)
		}
		out := tw.tempBytes[0:0]
		out = strconv.AppendFloat(out, value, 'g', -1, 64)
		tw.buf.Write(out)
	}
	return tw.buf.GetWriterError()
}

// String writes a JSON string.
func (tw *tokenWriter) String(value string) error {
	return tw.writeQuotedString(value)
}

// Raw writes a preformatted chunk of JSON data.
func (tw *tokenWriter) Raw(value json.RawMessage) error {
	tw.buf.Write(value)
	return tw.buf.GetWriterError()
}

// PropertyName writes a JSON object property name followed by a colon.
func (tw *tokenWriter) PropertyName(name string) error {
	if err := tw.String(name); err != nil {
		return err
	}
	tw.buf.WriteByte(':')
	return tw.buf.GetWriterError()
}

// Delimiter writes a single character which must be a valid JSON delimiter ('{', ',', etc.).
func (tw *tokenWriter) Delimiter(delimiter byte) error {
	tw.buf.WriteByte(delimiter)
	return tw.buf.GetWriterError()
}

func (tw *tokenWriter) writeQuotedString(s string) error {
	// This is basically the same logic used internally by json.Marshal
	tw.buf.WriteByte('"')
	start := 0
	for i := 0; i < len(s); {
		aByte := s[i]
		if aByte < ' ' || aByte == '"' || aByte == '\\' {
			if i > start {
				tw.buf.WriteString(s[start:i])
			}
			tw.writeEscapedChar(aByte)
			i++
			start = i
		} else {
			if aByte < utf8.RuneSelf { // single-byte character
				i++
			} else {
				_, size := utf8.DecodeRuneInString(s[i:])
				i += size
			}
		}
	}
	if start < len(s) {
		tw.buf.WriteString(s[start:])
	}
	tw.buf.WriteByte('"')
	return tw.buf.GetWriterError()
}

func (tw *tokenWriter) writeEscapedChar(ch byte) {
	out := tw.tempBytes[0:2]
	out[0] = '\\'
	switch ch {
	case '\b':
		out[1] = 'b'
	case '\t':
		out[1] = 't'
	case '\n':
		out[1] = 'n'
	case '\f':
		out[1] = 'f'
	case '\r':
		out[1] = 'r'
	case '"':
		out[1] = '"'
	case '\\':
		out[1] = '\\'
	default:
		out[1] = 'u'
		out = append(out, '0')
		out = append(out, '0')
		hexChars := make([]byte, 0, 4)
		hexChars = strconv.AppendInt(hexChars, int64(ch), 16)
		if len(hexChars) < 2 {
			out = append(out, '0')
		}
		out = append(out, hexChars...)
	}
	tw.buf.Write(out)
}
