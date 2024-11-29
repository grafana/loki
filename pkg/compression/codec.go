package compression

import (
	"fmt"
	"strings"
)

// Codec identifies an available compression codec.
type Codec byte

// The different available codecs
// Make sure to preserve the order, as the numeric values are serialized!
//
//nolint:revive
const (
	None Codec = iota
	GZIP
	Dumb // not supported
	LZ4_64k
	Snappy
	LZ4_256k
	LZ4_1M
	LZ4_4M
	Flate
	Zstd
)

var supportedCodecs = []Codec{
	None,
	GZIP,
	LZ4_64k,
	Snappy,
	LZ4_256k,
	LZ4_1M,
	LZ4_4M,
	Flate,
	Zstd,
}

func (e Codec) String() string {
	switch e {
	case GZIP:
		return "gzip"
	case None:
		return "none"
	case LZ4_64k:
		return "lz4-64k"
	case LZ4_256k:
		return "lz4-256k"
	case LZ4_1M:
		return "lz4-1M"
	case LZ4_4M:
		return "lz4"
	case Snappy:
		return "snappy"
	case Flate:
		return "flate"
	case Zstd:
		return "zstd"
	default:
		return "unknown"
	}
}

// ParseCodec parses a chunk encoding (compression codec) by its name.
func ParseCodec(enc string) (Codec, error) {
	for _, e := range supportedCodecs {
		if strings.EqualFold(e.String(), enc) {
			return e, nil
		}
	}
	return 0, fmt.Errorf("invalid encoding: %s, supported: %s", enc, SupportedCodecs())
}

// SupportedCodecs returns the list of supported Encoding.
func SupportedCodecs() string {
	var sb strings.Builder
	for i := range supportedCodecs {
		sb.WriteString(supportedCodecs[i].String())
		if i != len(supportedCodecs)-1 {
			sb.WriteString(", ")
		}
	}
	return sb.String()
}
