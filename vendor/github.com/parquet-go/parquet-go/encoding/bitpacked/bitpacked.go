package bitpacked

import (
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type Encoding struct {
	encoding.NotSupported
	BitWidth int
}

func (e *Encoding) String() string {
	return "BIT_PACKED"
}

func (e *Encoding) Encoding() format.Encoding {
	return format.BitPacked
}

func (e *Encoding) EncodeLevels(dst []byte, src []uint8) ([]byte, error) {
	dst, err := encodeLevels(dst[:0], src, uint(e.BitWidth))
	return dst, e.wrap(err)
}

func (e *Encoding) DecodeLevels(dst []uint8, src []byte) ([]uint8, error) {
	dst, err := decodeLevels(dst[:0], src, uint(e.BitWidth))
	return dst, e.wrap(err)
}

func (e *Encoding) wrap(err error) error {
	if err != nil {
		err = encoding.Error(e, err)
	}
	return err
}

func encodeLevels(dst, src []byte, bitWidth uint) ([]byte, error) {
	if bitWidth == 0 || len(src) == 0 {
		return append(dst[:0], 0), nil
	}

	n := ((int(bitWidth) * len(src)) + 7) / 8
	c := n + 1

	if cap(dst) < c {
		dst = make([]byte, c, 2*c)
	} else {
		dst = dst[:c]
		for i := range dst {
			dst[i] = 0
		}
	}

	bitMask := byte(1<<bitWidth) - 1
	bitShift := 8 - bitWidth
	bitOffset := uint(0)

	for _, value := range src {
		v := bitFlip(value) >> bitShift
		i := bitOffset / 8
		j := bitOffset % 8
		dst[i+0] |= (v & bitMask) << j
		dst[i+1] |= (v >> (8 - j))
		bitOffset += bitWidth
	}

	return dst[:n], nil
}

func decodeLevels(dst, src []byte, bitWidth uint) ([]byte, error) {
	if bitWidth == 0 || len(src) == 0 {
		return append(dst[:0], 0), nil
	}

	numBits := 8 * uint(len(src))
	numValues := int(numBits / bitWidth)
	if (numBits % bitWidth) != 0 {
		numValues++
	}

	if cap(dst) < numValues {
		dst = make([]byte, numValues, 2*numValues)
	} else {
		dst = dst[:numValues]
		for i := range dst {
			dst[i] = 0
		}
	}

	bitMask := byte(1<<bitWidth) - 1
	bitShift := 8 - bitWidth
	bitOffset := uint(0)

	for k := range dst {
		i := bitOffset / 8
		j := bitOffset % 8
		v := (src[i+0] >> j)
		if int(i+1) < len(src) {
			v |= (src[i+1] << (8 - j))
		}
		v &= bitMask
		dst[k] = bitFlip(v) >> bitShift
		bitOffset += bitWidth
	}

	return dst, nil
}

func bitFlip(b byte) byte {
	return (((b >> 0) & 1) << 7) |
		(((b >> 1) & 1) << 6) |
		(((b >> 2) & 1) << 5) |
		(((b >> 3) & 1) << 4) |
		(((b >> 4) & 1) << 3) |
		(((b >> 5) & 1) << 2) |
		(((b >> 6) & 1) << 1) |
		(((b >> 7) & 1) << 0)
}
