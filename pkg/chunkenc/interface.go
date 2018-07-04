package chunkenc

import (
	"io"
)

// Encoding is the identifier for a chunk encoding.
type Encoding uint8

// The different available encodings.
const (
	EncNone Encoding = iota
	EncGZIP
)

func (e Encoding) String() string {
	switch e {
	case EncGZIP:
		return "gzip"
	case EncNone:
		return "none"
	default:
		return "unknown"
	}
}

// CompressionWriter is the writer that compresses the data passed to it.
type CompressionWriter interface {
	Write(p []byte) (int, error)
	Close() error
	Flush() error
	Reset(w io.Writer)
}

// CompressionReader reads the compressed data.
type CompressionReader interface {
	Read(p []byte) (int, error)
	Reset(r io.Reader) error
}
