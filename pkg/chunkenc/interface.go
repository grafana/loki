package chunkenc

import (
	"errors"
	"fmt"
	"strings"
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
type Encoding byte

// The different available encodings.
const (
	EncNone Encoding = iota
	EncGZIP
	EncDumb
	EncLZ4
	EncSnappy
)

var supportedEncoding = []Encoding{
	EncGZIP,
	EncLZ4,
	EncSnappy,
}

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
	case EncSnappy:
		return "snappy"
	default:
		return "unknown"
	}
}

// ParseEncoding parses an chunk encoding (compression algorithm) by its name.
func ParseEncoding(enc string) (Encoding, error) {
	for _, e := range supportedEncoding {
		if strings.EqualFold(e.String(), enc) {
			return e, nil
		}
	}
	return 0, fmt.Errorf("invalid encoding: %s, supported: %s", enc, SupportedEncoding())

}

// SupportedEncoding returns the list of supported Encoding.
func SupportedEncoding() string {
	var sb strings.Builder
	for i := range supportedEncoding {
		sb.WriteString(supportedEncoding[i].String())
		if i != len(supportedEncoding)-1 {
			sb.WriteString(", ")
		}
	}
	return sb.String()
}

// Chunk is the interface for the compressed logs chunk format.
type Chunk interface {
	Bounds() (time.Time, time.Time)
	SpaceFor(*logproto.Entry) bool
	Append(*logproto.Entry) error
	Iterator(from, through time.Time, direction logproto.Direction, filter logql.Filter) (iter.EntryIterator, error)
	Size() int
	Bytes() ([]byte, error)
	Blocks() int
	Utilization() float64
	UncompressedSize() int
	CompressedSize() int
	Close() error
}
