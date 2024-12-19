// Package uncompressed provides implementations of the compression codec
// interfaces as pass-through without applying any compression nor
// decompression.
package uncompressed

import (
	"github.com/parquet-go/parquet-go/format"
)

type Codec struct {
}

func (c *Codec) String() string {
	return "UNCOMPRESSED"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Uncompressed
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	return append(dst[:0], src...), nil
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	return append(dst[:0], src...), nil
}
