package chunkenc

import (
	"io"
	"net/http"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
)

// Errors returned by the chunk interface.
var (
	ErrChunkFull       = util.NewCodedError(http.StatusInternalServerError, "chunk full")
	ErrOutOfOrder      = util.NewCodedError(http.StatusBadRequest, "entry out of order")
	ErrInvalidSize     = util.NewCodedError(http.StatusInternalServerError, "invalid size")
	ErrInvalidFlag     = util.NewCodedError(http.StatusInternalServerError, "invalid flag")
	ErrInvalidChecksum = util.NewCodedError(http.StatusInternalServerError, "invalid checksum")
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
