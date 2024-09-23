package compression

import "fmt"

const (
	ExtNone   = ""
	ExtGZIP   = ".gz"
	ExtSnappy = ".sz"
	ExtLZ4    = ".lz4"
	ExtFlate  = ".zz"
	ExtZstd   = ".zst"
)

func ToFileExtension(e Encoding) string {
	switch e {
	case EncNone:
		return ExtNone
	case EncGZIP:
		return ExtGZIP
	case EncLZ4_64k, EncLZ4_256k, EncLZ4_1M, EncLZ4_4M:
		return ExtLZ4
	case EncSnappy:
		return ExtSnappy
	case EncFlate:
		return ExtFlate
	case EncZstd:
		return ExtZstd
	default:
		panic(fmt.Sprintf("invalid encoding: %d, supported: %s", e, SupportedEncoding()))
	}
}

func FromFileExtension(ext string) Encoding {
	switch ext {
	case ExtNone:
		return EncNone
	case ExtGZIP:
		return EncGZIP
	case ExtLZ4:
		return EncLZ4_4M
	case ExtSnappy:
		return EncSnappy
	case ExtFlate:
		return EncFlate
	case ExtZstd:
		return EncZstd
	default:
		panic(fmt.Sprintf("invalid file extension: %s", ext))
	}
}
