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

	bitOffset := uint(0)

	for _, value := range src {
		i := bitOffset / 8
		j := bitOffset % 8
		available := 8 - j
		if bitWidth <= available {
			// Value fits entirely in byte i; align to the right within available bits.
			dst[i] |= value << (available - bitWidth)
		} else {
			// Split across byte i and byte i+1.
			dst[i] |= value >> (bitWidth - available)
			dst[i+1] |= value << (8 - (bitWidth - available))
		}
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
	bitOffset := uint(0)

	for k := range dst {
		i := bitOffset / 8
		j := bitOffset % 8
		available := 8 - j
		var v byte
		if bitWidth <= available {
			v = (src[i] >> (available - bitWidth)) & bitMask
		} else {
			remaining := bitWidth - available
			topBits := src[i] & ((1 << available) - 1)
			var bottomBits byte
			if int(i+1) < len(src) {
				bottomBits = src[i+1] >> (8 - remaining)
			}
			v = (topBits << remaining) | bottomBits
		}
		dst[k] = v
		bitOffset += bitWidth
	}

	return dst, nil
}
