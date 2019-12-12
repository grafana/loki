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
	EncGZIPBestSpeed
	EncDumb
	EncLZ4
	EncSnappy
	EncSnappyV2
)

func (e Encoding) String() string {
	switch e {
	case EncGZIP:
		return "gzip"
	case EncGZIPBestSpeed:
		return "gzip-1"
	case EncNone:
		return "none"
	case EncDumb:
		return "dumb"
	case EncLZ4:
		return "lz4"
	case EncSnappy:
		return "snappy"
	case EncSnappyV2:
		return "snappyv2"
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
