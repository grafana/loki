// Package snappy implements the SNAPPY parquet compression codec.
package snappy

import (
	"github.com/klauspost/compress/snappy"
	"github.com/parquet-go/parquet-go/format"
)

type Codec struct {
}

// The snappy.Reader and snappy.Writer implement snappy encoding/decoding with
// a framing protocol, but snappy requires the implementation to use the raw
// snappy block encoding. This is why we need to use snappy.Encode/snappy.Decode
// and have to ship custom implementations of the compressed reader and writer.

func (c *Codec) String() string {
	return "SNAPPY"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Snappy
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	return snappy.Encode(dst, src), nil
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	return snappy.Decode(dst, src)
}
