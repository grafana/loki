package rle

import (
	"math/bits"

	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

type DictionaryEncoding struct {
	encoding.NotSupported
}

func (e *DictionaryEncoding) String() string {
	return "RLE_DICTIONARY"
}

func (e *DictionaryEncoding) Encoding() format.Encoding {
	return format.RLEDictionary
}

func (e *DictionaryEncoding) EncodeInt32(dst []byte, src []int32) ([]byte, error) {
	bitWidth := maxLenInt32(src)
	dst = append(dst[:0], byte(bitWidth))
	dst, err := encodeInt32(dst, src, uint(bitWidth))
	return dst, e.wrap(err)
}

func (e *DictionaryEncoding) DecodeInt32(dst []int32, src []byte) ([]int32, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}
	buf := unsafecast.Slice[byte](dst)
	buf, err := decodeInt32(buf[:0], src[1:], uint(src[0]))
	return unsafecast.Slice[int32](buf), e.wrap(err)
}

func (e *DictionaryEncoding) wrap(err error) error {
	if err != nil {
		err = encoding.Error(e, err)
	}
	return err
}

func clearInt32(data []int32) {
	for i := range data {
		data[i] = 0
	}
}

func maxLenInt32(data []int32) (max int) {
	for _, v := range data {
		if n := bits.Len32(uint32(v)); n > max {
			max = n
		}
	}
	return max
}
