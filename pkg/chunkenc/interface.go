package chunkenc

import (
	"errors"
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
	EncLZ4
)

func (e Encoding) String() string {
	switch e {
	case EncGZIP:
		return "gzip"
	case EncNone:
		return "none"
	case EncDumb:
		return "dumb"
	case EncLZ4:
		return "lz4"
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
