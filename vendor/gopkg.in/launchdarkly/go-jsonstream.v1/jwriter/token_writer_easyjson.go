// +build launchdarkly_easyjson

package jwriter

// This file defines the easyjson-based implementation of the low-level JSON writer, which is used instead
// of token_writer_default.go if the launchdarkly_easyjson build tag is enabled.
//
// For the contract governing the behavior of the exported methods in this type, see the comments on the
// corresponding methods in token_writer_default.go.

import (
	"encoding/json"
	"io"

	ejwriter "github.com/mailru/easyjson/jwriter"
)

var nullToken = []byte("null")

type tokenWriter struct {
	// We might be initialized either with a pointer to an existing Writer, in which case we'll use that.
	pWriter *ejwriter.Writer
	// Or, we might be initialized from scratch so we must create our own Writer. We'd like to avoid
	// allocating that on the heap, so we'll store it here.
	inlineWriter     ejwriter.Writer
	targetIOWriter   io.Writer
	targetBufferSize int
}

func newTokenWriter() tokenWriter {
	return tokenWriter{}
}

func newTokenWriterFromEasyjsonWriter(writer *ejwriter.Writer) tokenWriter {
	return tokenWriter{pWriter: writer}
}

func newStreamingTokenWriter(dest io.Writer, bufferSize int) tokenWriter {
	tw := tokenWriter{targetIOWriter: dest, targetBufferSize: bufferSize}
	tw.inlineWriter.Buffer.EnsureSpace(bufferSize)
	return tw
}

func (tw *tokenWriter) Bytes() []byte {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	bytes, _ := pWriter.BuildBytes(nil)
	return bytes
}

func (tw *tokenWriter) Grow(n int) {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Buffer.EnsureSpace(n)
}

func (tw *tokenWriter) Flush() error {
	if tw.targetIOWriter == nil {
		return nil
	}
	_, err := tw.inlineWriter.DumpTo(tw.targetIOWriter)
	return err
}

func (tw *tokenWriter) Null() error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Raw(nullToken, nil)
	return tw.maybeFlush()
}

func (tw *tokenWriter) Bool(value bool) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Bool(value)
	return tw.maybeFlush()
}

func (tw *tokenWriter) Int(value int) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Int(value)
	return tw.maybeFlush()
}

func (tw *tokenWriter) Float64(value float64) error {
	i := int(value)
	if float64(i) == value {
		return tw.Int(i)
	}
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Float64(value)
	return tw.maybeFlush()
}

func (tw *tokenWriter) String(value string) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.String(value)
	return tw.maybeFlush()
}

func (tw *tokenWriter) Raw(value json.RawMessage) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.Raw(value, nil)
	return tw.maybeFlush()
}

func (tw *tokenWriter) PropertyName(name string) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.String(name)
	pWriter.RawByte(':')
	return tw.maybeFlush()
}

func (tw *tokenWriter) Delimiter(delimiter byte) error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	pWriter.RawByte(delimiter)
	return tw.maybeFlush()
}

func (tw *tokenWriter) maybeFlush() error {
	pWriter := tw.pWriter
	if pWriter == nil {
		pWriter = &tw.inlineWriter
	}
	if pWriter.Error != nil {
		return pWriter.Error
	}
	if tw.targetIOWriter == nil || pWriter.Buffer.Size() < tw.targetBufferSize {
		return nil
	}
	return tw.Flush()
}
