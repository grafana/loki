package chunkenc

import "bufio"

// unrolledDecodeUVarint decodes a unsigned varint from the provided buffer.
// This is the same as the std binary library except that it avoids a for loop.
// see https://cs.opensource.google/go/go/+/master:src/encoding/binary/varint.go
func unrolledDecodeUVarint(buf *bufio.Reader) (uint64, error) {
	var by byte
	var err error
	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}
	b := uint64(by)
	if b < 0x80 {
		return b, nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}
	x := b & 0x7f
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 7), nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}

	x |= (b & 0x7f) << 7
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 14), nil
	}
	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}

	x |= (b & 0x7f) << 14
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 21), nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}
	x |= (b & 0x7f) << 21
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 28), nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}

	x |= (b & 0x7f) << 28
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 35), nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}
	x |= (b & 0x7f) << 35
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 42), nil
	}

	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}

	x |= (b & 0x7f) << 42
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 49), nil
	}
	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}

	x |= (b & 0x7f) << 49
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 56), nil
	}
	by, err = buf.ReadByte()
	if err != nil {
		return 0, err
	}
	x |= (b & 0x7f) << 56
	b = uint64(by)
	if b < 0x80 {
		return x | (b << 63), nil
	}

	return 0, nil
}

// unrolledDecodeVarint decodes a signed varint from the provided buffer.
// This is the same as the std binary library except that it avoids a for loop.
// see https://cs.opensource.google/go/go/+/master:src/encoding/binary/varint.go
func unrolledDecodeVarint(buf *bufio.Reader) (int64, error) {
	ux, err := unrolledDecodeUVarint(buf) // ok to continue in presence of error
	x := int64(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x, err
}
