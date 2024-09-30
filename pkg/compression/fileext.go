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

func ToFileExtension(e Codec) string {
	switch e {
	case None:
		return ExtNone
	case GZIP:
		return ExtGZIP
	case LZ4_64k, LZ4_256k, LZ4_1M, LZ4_4M:
		return ExtLZ4
	case Snappy:
		return ExtSnappy
	case Flate:
		return ExtFlate
	case Zstd:
		return ExtZstd
	default:
		panic(fmt.Sprintf("invalid codec: %d, supported: %s", e, SupportedCodecs()))
	}
}

func FromFileExtension(ext string) Codec {
	switch ext {
	case ExtNone:
		return None
	case ExtGZIP:
		return GZIP
	case ExtLZ4:
		return LZ4_4M
	case ExtSnappy:
		return Snappy
	case ExtFlate:
		return Flate
	case ExtZstd:
		return Zstd
	default:
		panic(fmt.Sprintf("invalid file extension: %s", ext))
	}
}
