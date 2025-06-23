package intcomp

import "math/bits"

const (
	BitPackingBlockSize32 = 128
	BitPackingBlockSize64 = 256
)

// CompressDeltaBinPackInt32 compress blocks of 128 integers from `in`
// and append to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not compressed (could
// not fit into a block), and the updated `out` slice.
//
// Compression logic:
//  1. Difference of consecutive inputs is computed (differential coding)
//  2. ZigZag encoding is applied if a block contains at least one negative delta value
//  3. The result is bit packed into the optimal number of bits for the block
func CompressDeltaBinPackInt32(in []int32, out []uint32) ([]int32, []uint32) {
	blockN := len(in) / BitPackingBlockSize32
	if blockN == 0 {
		// input less than block size
		return in, out
	}

	if out == nil {
		out = make([]uint32, 0, len(in)/2)
	}
	// skip header (written at the end)
	headerpos := len(out)
	outpos := headerpos + 3

	initoffset := in[0]

	for blockI := 0; blockI < blockN; blockI++ {
		const groupSize = BitPackingBlockSize32 / 4
		i := blockI * BitPackingBlockSize32
		group1 := in[i+0*groupSize : i+1*groupSize]
		group2 := in[i+1*groupSize : i+2*groupSize]
		group3 := in[i+2*groupSize : i+3*groupSize]
		group4 := in[i+3*groupSize : i+4*groupSize]

		bitlen1, sign1 := deltaBitLenAndSignInt32(initoffset, (*[32]int32)(group1))
		bitlen2, sign2 := deltaBitLenAndSignInt32(group1[31], (*[32]int32)(group2))
		bitlen3, sign3 := deltaBitLenAndSignInt32(group2[31], (*[32]int32)(group3))
		bitlen4, sign4 := deltaBitLenAndSignInt32(group3[31], (*[32]int32)(group4))

		if l := bitlen1 + bitlen2 + bitlen3 + bitlen4 + 1; outpos+l < cap(out) {
			out = out[:outpos+l]
		} else {
			if min := len(out) / 4; l < min {
				l = min
			}
			grow := make([]uint32, outpos+l)
			copy(grow, out)
			out = grow
		}

		// write block header
		out[outpos] = uint32((sign1 << 31) | (bitlen1 << 24) |
			(sign2 << 23) | (bitlen2 << 16) |
			(sign3 << 15) | (bitlen3 << 8) |
			(sign4 << 7) | bitlen4)
		outpos++

		// write groups (4 x 32 packed inputs)
		if sign1 == 0 {
			deltaPack_int32(initoffset, group1, out[outpos:], bitlen1)
		} else {
			deltaPackZigzag_int32(initoffset, group1, out[outpos:], bitlen1)
		}
		outpos += bitlen1

		if sign2 == 0 {
			deltaPack_int32(group1[31], group2, out[outpos:], bitlen2)
		} else {
			deltaPackZigzag_int32(group1[31], group2, out[outpos:], bitlen2)
		}
		outpos += bitlen2

		if sign3 == 0 {
			deltaPack_int32(group2[31], group3, out[outpos:], bitlen3)
		} else {
			deltaPackZigzag_int32(group2[31], group3, out[outpos:], bitlen3)
		}
		outpos += bitlen3

		if sign4 == 0 {
			deltaPack_int32(group3[31], group4, out[outpos:], bitlen4)
		} else {
			deltaPackZigzag_int32(group3[31], group4, out[outpos:], bitlen4)
		}
		outpos += bitlen4

		initoffset = group4[31]
	}

	// write header
	out[headerpos] = uint32(blockN * BitPackingBlockSize32)
	out[headerpos+1] = uint32(outpos - headerpos)
	out[headerpos+2] = uint32(in[0])
	return in[blockN*BitPackingBlockSize32:], out[:outpos]
}

// UncompressDeltaBinPackInt32 uncompress one ore more blocks of 128 integers from `in`
// and append the result to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not uncompressed, and the updated `out` slice.
func UncompressDeltaBinPackInt32(in []uint32, out []int32) ([]uint32, []int32) {
	if len(in) == 0 {
		return in, out
	}
	// read header
	initoffset := int32(in[2])
	inpos := 3

	// output fix
	outpos := len(out)
	if l := int(uint(in[0])); l <= cap(out)-len(out) {
		out = out[:len(out)+l]
	} else {
		grow := make([]int32, len(out)+l)
		copy(grow, out)
		out = grow
	}

	for ; outpos < len(out); outpos += BitPackingBlockSize32 {
		const groupSize = BitPackingBlockSize32 / 4

		tmp := uint32(in[inpos])
		sign1 := int(tmp>>31) & 0x1
		sign2 := int(tmp>>23) & 0x1
		sign3 := int(tmp>>15) & 0x1
		sign4 := int(tmp>>7) & 0x1
		bitlen1 := int(tmp>>24) & 0x7F
		bitlen2 := int(tmp>>16) & 0x7F
		bitlen3 := int(tmp>>8) & 0x7F
		bitlen4 := int(tmp & 0x7F)
		inpos++

		if sign1 == 0 {
			deltaUnpack_int32(initoffset, in[inpos:], out[outpos:], bitlen1)
		} else {
			deltaUnpackZigzag_int32(initoffset, in[inpos:], out[outpos:], bitlen1)
		}
		inpos += int(bitlen1)
		initoffset = out[outpos+groupSize-1]

		if sign2 == 0 {
			deltaUnpack_int32(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		} else {
			deltaUnpackZigzag_int32(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		}
		inpos += int(bitlen2)
		initoffset = out[outpos+2*groupSize-1]

		if sign3 == 0 {
			deltaUnpack_int32(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		} else {
			deltaUnpackZigzag_int32(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		}
		inpos += int(bitlen3)
		initoffset = out[outpos+3*groupSize-1]

		if sign4 == 0 {
			deltaUnpack_int32(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		} else {
			deltaUnpackZigzag_int32(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		}
		inpos += int(bitlen4)
		initoffset = out[outpos+4*groupSize-1]
	}

	return in[inpos:], out
}

func deltaBitLenAndSignInt32(initoffset int32, buf *[32]int32) (int, int) {
	var mask uint32

	{
		const base = 0
		d0 := int32(buf[base] - initoffset)
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask = m0 | m1
	}
	{
		const base = 4
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 8
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 12
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 16
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 20
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 24
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 28
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}

	sign := int(mask & 1)
	// remove sign in zigzag encoding
	mask >>= 1

	return bits.Len32(uint32(mask)) + sign, sign
}

// CompressDeltaBinPackUint32 compress blocks of 128 integers from `in`
// and append to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not compressed (could
// not fit into a block), and the updated `out` slice.
//
// Compression logic:
//  1. Difference of consecutive inputs is computed (differential coding)
//  2. ZigZag encoding is applied if a block contains at least one negative delta value
//  3. The result is bit packed into the optimal number of bits for the block
func CompressDeltaBinPackUint32(in, out []uint32) ([]uint32, []uint32) {
	blockN := len(in) / BitPackingBlockSize32
	if blockN == 0 {
		// input less than block size
		return in, out
	}

	if out == nil {
		out = make([]uint32, 0, len(in)/2)
	}
	// skip header (written at the end)
	headerpos := len(out)
	outpos := headerpos + 3

	initoffset := in[0]

	for blockI := 0; blockI < blockN; blockI++ {
		const groupSize = BitPackingBlockSize32 / 4
		i := blockI * BitPackingBlockSize32
		group1 := in[i+0*groupSize : i+1*groupSize]
		group2 := in[i+1*groupSize : i+2*groupSize]
		group3 := in[i+2*groupSize : i+3*groupSize]
		group4 := in[i+3*groupSize : i+4*groupSize]

		bitlen1, sign1 := deltaBitLenAndSignUint32(initoffset, (*[32]uint32)(group1))
		bitlen2, sign2 := deltaBitLenAndSignUint32(group1[31], (*[32]uint32)(group2))
		bitlen3, sign3 := deltaBitLenAndSignUint32(group2[31], (*[32]uint32)(group3))
		bitlen4, sign4 := deltaBitLenAndSignUint32(group3[31], (*[32]uint32)(group4))

		if l := bitlen1 + bitlen2 + bitlen3 + bitlen4 + 1; outpos+l < cap(out) {
			out = out[:outpos+l]
		} else {
			if min := len(out) / 4; l < min {
				l = min
			}
			grow := make([]uint32, outpos+l)
			copy(grow, out)
			out = grow
		}

		// write block header
		out[outpos] = uint32((sign1 << 31) | (bitlen1 << 24) |
			(sign2 << 23) | (bitlen2 << 16) |
			(sign3 << 15) | (bitlen3 << 8) |
			(sign4 << 7) | bitlen4)
		outpos++

		// write groups (4 x 32 packed inputs)
		if sign1 == 0 {
			deltaPack_uint32(initoffset, group1, out[outpos:], bitlen1)
		} else {
			deltaPackZigzag_uint32(initoffset, group1, out[outpos:], bitlen1)
		}
		outpos += bitlen1

		if sign2 == 0 {
			deltaPack_uint32(group1[31], group2, out[outpos:], bitlen2)
		} else {
			deltaPackZigzag_uint32(group1[31], group2, out[outpos:], bitlen2)
		}
		outpos += bitlen2

		if sign3 == 0 {
			deltaPack_uint32(group2[31], group3, out[outpos:], bitlen3)
		} else {
			deltaPackZigzag_uint32(group2[31], group3, out[outpos:], bitlen3)
		}
		outpos += bitlen3

		if sign4 == 0 {
			deltaPack_uint32(group3[31], group4, out[outpos:], bitlen4)
		} else {
			deltaPackZigzag_uint32(group3[31], group4, out[outpos:], bitlen4)
		}
		outpos += bitlen4

		initoffset = group4[31]
	}

	// write header
	out[headerpos] = uint32(blockN * BitPackingBlockSize32)
	out[headerpos+1] = uint32(outpos - headerpos)
	out[headerpos+2] = in[0]
	return in[blockN*BitPackingBlockSize32:], out[:outpos]
}

// UncompressDeltaBinPackUint32 uncompress one ore more blocks of 128 integers from `in`
// and append the result to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not uncompressed, and the updated `out` slice.
func UncompressDeltaBinPackUint32(in, out []uint32) ([]uint32, []uint32) {
	if len(in) == 0 {
		return in, out
	}
	// read header
	initoffset := in[2]
	inpos := 3

	// output fix
	outpos := len(out)
	if l := int(uint(in[0])); l <= cap(out)-len(out) {
		out = out[:len(out)+l]
	} else {
		grow := make([]uint32, len(out)+l)
		copy(grow, out)
		out = grow
	}

	for ; outpos < len(out); outpos += BitPackingBlockSize32 {
		const groupSize = BitPackingBlockSize32 / 4

		tmp := uint32(in[inpos])
		sign1 := int(tmp>>31) & 0x1
		sign2 := int(tmp>>23) & 0x1
		sign3 := int(tmp>>15) & 0x1
		sign4 := int(tmp>>7) & 0x1
		bitlen1 := int(tmp>>24) & 0x7F
		bitlen2 := int(tmp>>16) & 0x7F
		bitlen3 := int(tmp>>8) & 0x7F
		bitlen4 := int(tmp & 0x7F)
		inpos++

		if sign1 == 0 {
			deltaUnpack_uint32(initoffset, in[inpos:], out[outpos:], bitlen1)
		} else {
			deltaUnpackZigzag_uint32(initoffset, in[inpos:], out[outpos:], bitlen1)
		}
		inpos += bitlen1
		initoffset = out[outpos+groupSize-1]

		if sign2 == 0 {
			deltaUnpack_uint32(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		} else {
			deltaUnpackZigzag_uint32(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		}
		inpos += bitlen2
		initoffset = out[outpos+2*groupSize-1]

		if sign3 == 0 {
			deltaUnpack_uint32(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		} else {
			deltaUnpackZigzag_uint32(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		}
		inpos += bitlen3
		initoffset = out[outpos+3*groupSize-1]

		if sign4 == 0 {
			deltaUnpack_uint32(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		} else {
			deltaUnpackZigzag_uint32(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		}
		inpos += bitlen4
		initoffset = out[outpos+4*groupSize-1]
	}

	return in[inpos:], out
}

func deltaBitLenAndSignUint32(initoffset uint32, buf *[32]uint32) (int, int) {
	var mask uint32

	{
		const base = 0
		d0 := int32(buf[base] - initoffset)
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask = m0 | m1
	}
	{
		const base = 4
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 8
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 12
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 16
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 20
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 24
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	{
		const base = 28
		d0 := int32(buf[base] - buf[base-1])
		d1 := int32(buf[base+1] - buf[base])
		d2 := int32(buf[base+2] - buf[base+1])
		d3 := int32(buf[base+3] - buf[base+2])
		m0 := uint32((d0<<1)^(d0>>31)) | uint32((d1<<1)^(d1>>31))
		m1 := uint32((d2<<1)^(d2>>31)) | uint32((d3<<1)^(d3>>31))
		mask |= m0 | m1
	}
	sign := int(mask & 1)
	// remove sign in zigzag encoding
	mask >>= 1

	return bits.Len32(uint32(mask)) + sign, sign
}

// CompressDeltaBinPackInt64 compress blocks of 256 integers from `in`
// and append to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not compressed (could
// not fit into a block), and the updated `out` slice.
//
// Compression logic:
//  1. Difference of consecutive inputs is computed (differential coding)
//  2. ZigZag encoding is applied if a block contains at least one negative delta value
//  3. The result is bit packed into the optimal number of bits for the block
func CompressDeltaBinPackInt64(in []int64, out []uint64) ([]int64, []uint64) {
	blockN := len(in) / BitPackingBlockSize64
	if blockN == 0 {
		// input less than block size
		return in, out
	}

	if out == nil {
		out = make([]uint64, 0, len(in)/2)
	}
	// skip header (written at the end)
	headerpos := len(out)
	outpos := headerpos + 2

	initoffset := in[0]

	for blockI := 0; blockI < blockN; blockI++ {
		const groupSize = BitPackingBlockSize64 / 4
		i := blockI * BitPackingBlockSize64
		group1 := in[i+0*groupSize : i+1*groupSize]
		group2 := in[i+1*groupSize : i+2*groupSize]
		group3 := in[i+2*groupSize : i+3*groupSize]
		group4 := in[i+3*groupSize : i+4*groupSize]

		ntz1, bitlen1, sign1 := deltaBitTzAndLenAndSignInt64(initoffset, group1)
		ntz2, bitlen2, sign2 := deltaBitTzAndLenAndSignInt64(group1[63], group2)
		ntz3, bitlen3, sign3 := deltaBitTzAndLenAndSignInt64(group2[63], group3)
		ntz4, bitlen4, sign4 := deltaBitTzAndLenAndSignInt64(group3[63], group4)

		if l := bitlen1 + bitlen2 + bitlen3 + bitlen4 + 1 - ntz1 - ntz2 - ntz3 - ntz4; outpos+l < cap(out) {
			out = out[:outpos+l]
		} else {
			if min := len(out) / 4; l < min {
				l = min
			}
			grow := make([]uint64, outpos+l)
			copy(grow, out)
			out = grow
		}

		// write block header (min/max bits)
		out[outpos] = uint64((ntz1 << 56) | (ntz2 << 48) | (ntz3 << 40) | (ntz4 << 32) |
			(sign1 << 31) | (bitlen1 << 24) |
			(sign2 << 23) | (bitlen2 << 16) |
			(sign3 << 15) | (bitlen3 << 8) |
			(sign4 << 7) | bitlen4)
		outpos++

		// write groups (4 x 64 packed inputs)
		if sign1 == 0 {
			deltaPack_int64(initoffset, group1, out[outpos:], ntz1, bitlen1)
		} else {
			deltaPackZigzag_int64(initoffset, group1, out[outpos:], ntz1, bitlen1)
		}
		outpos += int(bitlen1 - ntz1)

		if sign2 == 0 {
			deltaPack_int64(group1[63], group2, out[outpos:], ntz2, bitlen2)
		} else {
			deltaPackZigzag_int64(group1[63], group2, out[outpos:], ntz2, bitlen2)
		}
		outpos += int(bitlen2 - ntz2)

		if sign3 == 0 {
			deltaPack_int64(group2[63], group3, out[outpos:], ntz3, bitlen3)
		} else {
			deltaPackZigzag_int64(group2[63], group3, out[outpos:], ntz3, bitlen3)
		}
		outpos += int(bitlen3 - ntz3)

		if sign4 == 0 {
			deltaPack_int64(group3[63], group4, out[outpos:], ntz4, bitlen4)
		} else {
			deltaPackZigzag_int64(group3[63], group4, out[outpos:], ntz4, bitlen4)
		}
		outpos += int(bitlen4 - ntz4)

		initoffset = group4[63]
	}

	// write header
	out[headerpos] = uint64(blockN*BitPackingBlockSize64) | uint64(outpos-headerpos)<<32
	out[headerpos+1] = uint64(in[0])
	return in[blockN*BitPackingBlockSize64:], out[:outpos]
}

// UncompressDeltaBinPackInt64 uncompress one ore more blocks of 256 integers from `in`
// and append the result to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not uncompressed, and the updated `out` slice.
func UncompressDeltaBinPackInt64(in []uint64, out []int64) ([]uint64, []int64) {
	if len(in) == 0 {
		return in, out
	}
	// read header
	initoffset := int64(in[1])
	inpos := 2

	// output fix
	outpos := len(out)
	if l := int(uint32(in[0])); l <= cap(out)-len(out) {
		out = out[:len(out)+l]
	} else {
		grow := make([]int64, len(out)+l)
		copy(grow, out)
		out = grow
	}
	if len(in) == 0 {
		return in, out
	}

	for ; outpos < len(out); outpos += BitPackingBlockSize64 {
		const groupSize = BitPackingBlockSize64 / 4

		tmp := uint64(in[inpos])
		ntz1 := int(tmp>>56) & 0xFF
		ntz2 := int(tmp>>48) & 0xFF
		ntz3 := int(tmp>>40) & 0xFF
		ntz4 := int(tmp>>32) & 0xFF
		sign1 := int(tmp>>31) & 1
		sign2 := int(tmp>>23) & 1
		sign3 := int(tmp>>15) & 1
		sign4 := int(tmp>>7) & 1
		bitlen1 := int(tmp>>24) & 0x7F
		bitlen2 := int(tmp>>16) & 0x7F
		bitlen3 := int(tmp>>8) & 0x7F
		bitlen4 := int(tmp & 0x7F)
		inpos++

		if sign1 == 0 {
			deltaUnpack_int64(initoffset, in[inpos:], out[outpos:], ntz1, bitlen1)
		} else {
			deltaUnpackZigzag_int64(initoffset, in[inpos:], out[outpos:], ntz1, bitlen1)
		}
		inpos += int(bitlen1 - ntz1)
		initoffset = out[outpos+groupSize-1]

		if sign2 == 0 {
			deltaUnpack_int64(initoffset, in[inpos:], out[outpos+groupSize:], ntz2, bitlen2)
		} else {
			deltaUnpackZigzag_int64(initoffset, in[inpos:], out[outpos+groupSize:], ntz2, bitlen2)
		}
		inpos += int(bitlen2 - ntz2)
		initoffset = out[outpos+2*groupSize-1]

		if sign3 == 0 {
			deltaUnpack_int64(initoffset, in[inpos:], out[outpos+2*groupSize:], ntz3, bitlen3)
		} else {
			deltaUnpackZigzag_int64(initoffset, in[inpos:], out[outpos+2*groupSize:], ntz3, bitlen3)
		}
		inpos += int(bitlen3 - ntz3)
		initoffset = out[outpos+3*groupSize-1]

		if sign4 == 0 {
			deltaUnpack_int64(initoffset, in[inpos:], out[outpos+3*groupSize:], ntz4, bitlen4)
		} else {
			deltaUnpackZigzag_int64(initoffset, in[inpos:], out[outpos+3*groupSize:], ntz4, bitlen4)
		}
		inpos += int(bitlen4 - ntz4)
		initoffset = out[outpos+4*groupSize-1]
	}

	return in[inpos:], out
}

func deltaBitTzAndLenAndSignInt64(initoffset int64, inbuf []int64) (int, int, int) {
	var mask uint64

	for _, buf := range []*[32]int64{(*[32]int64)(inbuf[:32]), (*[32]int64)(inbuf[32:64])} {
		{
			const base = 0
			d0 := int64(buf[base] - initoffset)
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 4
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 8
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 12
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 16
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 20
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 24
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 28
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
			initoffset = buf[base+3]
		}
	}

	sign := int(mask & 1)
	// remove sign in zigzag encoding
	mask >>= 1

	var ntz int
	if mask != 0 && sign == 0 {
		ntz = bits.TrailingZeros64(mask)
	}

	return ntz, bits.Len64(uint64(mask)) + sign, sign
}

// CompressDeltaBinPackUint64 compress blocks of 256 integers from `in`
// and append to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not compressed (could
// not fit into a block), and the updated `out` slice.
//
// Compression logic:
//  1. Difference of consecutive inputs is computed (differential coding)
//  2. ZigZag encoding is applied if a block contains at least one negative delta value
//  3. The result is bit packed into the optimal number of bits for the block
func CompressDeltaBinPackUint64(in, out []uint64) ([]uint64, []uint64) {
	blockN := len(in) / BitPackingBlockSize64
	if blockN == 0 {
		// input less than block size
		return in, out
	}

	if out == nil {
		out = make([]uint64, 0, len(in)/2)
	}
	// skip header (written at the end)
	headerpos := len(out)
	outpos := headerpos + 2

	initoffset := in[0]

	for blockI := 0; blockI < blockN; blockI++ {
		const groupSize = BitPackingBlockSize64 / 4
		i := blockI * BitPackingBlockSize64
		group1 := in[i+0*groupSize : i+1*groupSize]
		group2 := in[i+1*groupSize : i+2*groupSize]
		group3 := in[i+2*groupSize : i+3*groupSize]
		group4 := in[i+3*groupSize : i+4*groupSize]

		bitlen1, sign1 := deltaBitLenAndSignUint64(initoffset, group1)
		bitlen2, sign2 := deltaBitLenAndSignUint64(group1[63], group2)
		bitlen3, sign3 := deltaBitLenAndSignUint64(group2[63], group3)
		bitlen4, sign4 := deltaBitLenAndSignUint64(group3[63], group4)

		if l := bitlen1 + bitlen2 + bitlen3 + bitlen4 + 1; outpos+l < cap(out) {
			out = out[:outpos+l]
		} else {
			if min := len(out) / 4; l < min {
				l = min
			}
			grow := make([]uint64, outpos+l)
			copy(grow, out)
			out = grow
		}

		// write block header (min/max bits)
		out[outpos] = uint64(
			(sign1 << 31) | (bitlen1 << 24) |
				(sign2 << 23) | (bitlen2 << 16) |
				(sign3 << 15) | (bitlen3 << 8) |
				(sign4 << 7) | bitlen4)
		outpos++

		// write groups (4 x 64 packed inputs)
		if sign1 == 0 {
			deltaPack_uint64(initoffset, group1, out[outpos:], bitlen1)
		} else {
			deltaPackZigzag_uint64(initoffset, group1, out[outpos:], bitlen1)
		}
		outpos += bitlen1

		if sign2 == 0 {
			deltaPack_uint64(group1[63], group2, out[outpos:], bitlen2)
		} else {
			deltaPackZigzag_uint64(group1[63], group2, out[outpos:], bitlen2)
		}
		outpos += bitlen2

		if sign3 == 0 {
			deltaPack_uint64(group2[63], group3, out[outpos:], bitlen3)
		} else {
			deltaPackZigzag_uint64(group2[63], group3, out[outpos:], bitlen3)
		}
		outpos += bitlen3

		if sign4 == 0 {
			deltaPack_uint64(group3[63], group4, out[outpos:], bitlen4)
		} else {
			deltaPackZigzag_uint64(group3[63], group4, out[outpos:], bitlen4)
		}
		outpos += bitlen4

		initoffset = group4[63]
	}

	// write header
	out[headerpos] = uint64(blockN*BitPackingBlockSize64) + uint64(outpos-headerpos)<<32
	out[headerpos+1] = in[0]
	return in[blockN*BitPackingBlockSize64:], out[:outpos]
}

// UncompressDeltaBinPackUint64 uncompress one ore more blocks of 256 integers from `in`
// and append the result to `out`. `out` slice will be resized if necessary.
// The function returns the values from `in` that were not uncompressed, and the updated `out` slice.
func UncompressDeltaBinPackUint64(in, out []uint64) ([]uint64, []uint64) {
	if len(in) == 0 {
		return in, out
	}
	// read header
	initoffset := in[1]
	inpos := 2

	// output fix
	outpos := len(out)
	if l := int(uint32(in[0])); l <= cap(out)-len(out) {
		out = out[:len(out)+l]
	} else {
		grow := make([]uint64, len(out)+l)
		copy(grow, out)
		out = grow
	}

	for ; outpos < len(out); outpos += BitPackingBlockSize64 {
		const groupSize = BitPackingBlockSize64 / 4

		tmp := uint64(in[inpos])
		sign1 := int(tmp>>31) & 1
		sign2 := int(tmp>>23) & 1
		sign3 := int(tmp>>15) & 1
		sign4 := int(tmp>>7) & 1
		bitlen1 := int(tmp>>24) & 0x7F
		bitlen2 := int(tmp>>16) & 0x7F
		bitlen3 := int(tmp>>8) & 0x7F
		bitlen4 := int(tmp & 0x7F)
		inpos++

		if sign1 == 0 {
			deltaUnpack_uint64(initoffset, in[inpos:], out[outpos:], bitlen1)
		} else {
			deltaUnpackZigzag_uint64(initoffset, in[inpos:], out[outpos:], bitlen1)
		}
		inpos += bitlen1
		initoffset = out[outpos+groupSize-1]

		if sign2 == 0 {
			deltaUnpack_uint64(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		} else {
			deltaUnpackZigzag_uint64(initoffset, in[inpos:], out[outpos+groupSize:], bitlen2)
		}
		inpos += bitlen2
		initoffset = out[outpos+2*groupSize-1]

		if sign3 == 0 {
			deltaUnpack_uint64(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		} else {
			deltaUnpackZigzag_uint64(initoffset, in[inpos:], out[outpos+2*groupSize:], bitlen3)
		}
		inpos += bitlen3
		initoffset = out[outpos+3*groupSize-1]

		if sign4 == 0 {
			deltaUnpack_uint64(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		} else {
			deltaUnpackZigzag_uint64(initoffset, in[inpos:], out[outpos+3*groupSize:], bitlen4)
		}
		inpos += bitlen4
		initoffset = out[outpos+4*groupSize-1]
	}

	return in[inpos:], out
}

func deltaBitLenAndSignUint64(initoffset uint64, inbuf []uint64) (int, int) {
	var mask uint64

	for _, buf := range []*[32]uint64{(*[32]uint64)(inbuf[:32]), (*[32]uint64)(inbuf[32:64])} {
		{
			const base = 0
			d0 := int64(buf[base] - initoffset)
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 4
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 8
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 12
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 16
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 20
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 24
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
		}
		{
			const base = 28
			d0 := int64(buf[base] - buf[base-1])
			d1 := int64(buf[base+1] - buf[base])
			d2 := int64(buf[base+2] - buf[base+1])
			d3 := int64(buf[base+3] - buf[base+2])
			m0 := uint64((d0<<1)^(d0>>63)) | uint64((d1<<1)^(d1>>63))
			m1 := uint64((d2<<1)^(d2>>63)) | uint64((d3<<1)^(d3>>63))
			mask |= m0 | m1
			initoffset = buf[base+3]
		}
	}

	sign := int(mask & 1)
	// remove sign in zigzag encoding
	mask >>= 1

	return bits.Len64(uint64(mask)) + sign, sign
}
