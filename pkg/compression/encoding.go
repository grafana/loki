package compression

import (
	"fmt"
	"strings"
)

// Encoding is the identifier for a chunk encoding.
type Encoding byte

// The different available encodings.
// Make sure to preserve the order, as these numeric values are written to the chunks!
const (
	EncNone Encoding = iota
	EncGZIP
	EncDumb
	EncLZ4_64k
	EncSnappy
	EncLZ4_256k
	EncLZ4_1M
	EncLZ4_4M
	EncFlate
	EncZstd
)

var supportedEncoding = []Encoding{
	EncNone,
	EncGZIP,
	EncLZ4_64k,
	EncSnappy,
	EncLZ4_256k,
	EncLZ4_1M,
	EncLZ4_4M,
	EncFlate,
	EncZstd,
}

func (e Encoding) String() string {
	switch e {
	case EncGZIP:
		return "gzip"
	case EncNone:
		return "none"
	case EncDumb:
		return "dumb"
	case EncLZ4_64k:
		return "lz4-64k"
	case EncLZ4_256k:
		return "lz4-256k"
	case EncLZ4_1M:
		return "lz4-1M"
	case EncLZ4_4M:
		return "lz4"
	case EncSnappy:
		return "snappy"
	case EncFlate:
		return "flate"
	case EncZstd:
		return "zstd"
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
