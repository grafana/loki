package chunkenc

import (
	"errors"
	"io"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// Errors returned by the chunk interface.
var (
	ErrChunkFull       = errors.New("chunk full")
	ErrOutOfOrder      = errors.New("entry out of order")
	ErrInvalidSize     = errors.New("invalid size")
	ErrInvalidFlag     = errors.New("invalid flag")
	ErrInvalidChecksum = errors.New("invalid checksum")
)

// Encoding is the identifier for a chunk encoding.
type Encoding uint8

// The different available encodings.
const (
	EncNone Encoding = iota
	EncGZIP
	EncDumb
)

func (e Encoding) String() string {
	switch e {
	case EncGZIP:
		return "gzip"
	case EncNone:
		return "none"
	case EncDumb:
		return "dumb"
	default:
		return "unknown"
	}
}

// Chunk is the interface for the compressed logs chunk format.
type Chunk interface {
	Bounds() (time.Time, time.Time)
	SpaceFor(*logproto.Entry) bool
	Append(*logproto.Entry) error
	Iterator(from, through time.Time, direction logproto.Direction, filter logql.Filter) (iter.EntryIterator, error)
	Size() int
	Bytes() ([]byte, error)
	Utilization() float64
	UncompressedSize() int
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
