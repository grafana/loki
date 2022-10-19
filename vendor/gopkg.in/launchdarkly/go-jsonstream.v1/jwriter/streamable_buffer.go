package jwriter

import (
	"bytes"
	"io"
)

type streamableBuffer struct {
	buf       bytes.Buffer
	dest      io.Writer
	destErr   error
	chunkSize int
}

func (b *streamableBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *streamableBuffer) Grow(n int) {
	b.buf.Grow(n)
}

func (b *streamableBuffer) SetStreamingWriter(w io.Writer, chunkSize int) {
	b.dest = w
	b.chunkSize = chunkSize
}

func (b *streamableBuffer) Flush() error {
	if b.dest != nil {
		if b.buf.Len() > 0 {
			if b.destErr == nil {
				data := b.buf.Bytes()
				_, b.destErr = b.dest.Write(data)
			}
			b.buf.Reset()
			return b.destErr
		}
	}
	return nil
}

func (b *streamableBuffer) maybeFlush() {
	if b.dest != nil && b.buf.Len() >= b.chunkSize {
		_ = b.Flush()
	}
}

func (b *streamableBuffer) GetWriterError() error {
	return b.destErr
}

func (b *streamableBuffer) Write(data []byte) {
	_, _ = b.buf.Write(data)
	b.maybeFlush()
}

func (b *streamableBuffer) WriteByte(data byte) { //nolint:govet
	_ = b.buf.WriteByte(data)
	b.maybeFlush()
}

func (b *streamableBuffer) WriteRune(ch rune) {
	_, _ = b.buf.WriteRune(ch)
	b.maybeFlush()
}

func (b *streamableBuffer) WriteString(s string) {
	_, _ = b.buf.WriteString(s)
	b.maybeFlush()
}
