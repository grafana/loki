//go:build goexperiment.simd

package delta

import (
	"math/bits"
	"simd/archsimd"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file provides implementations of the DELTA_BYTE_ARRAY kernels based
// on the simd/archsimd package, replacing the hand-written assembly of
// byte_array_amd64.s when GOEXPERIMENT=simd is set.

// validatePrefixAndSuffixLengthValuesSIMD checks 8 length pairs per
// iteration: negative values are accumulated with a running Or and tested
// once, and the prefix lengths are compared against the previous value
// lengths built with a rotate and carry blend. Any violation returns
// ok=false and the scalar path reruns to identify the exact error.
func validatePrefixAndSuffixLengthValuesSIMD(prefix, suffix []int32, maxLength int) (totalPrefixLength, totalSuffixLength int, ok bool) {
	n := min(len(prefix), len(suffix))
	i := 0
	lastValueLength := int32(0)

	if n >= 8 {
		zero := archsimd.BroadcastInt32x8(0)
		rot := archsimd.LoadUint32x8Slice(rotate1x8x32Delta[:])
		bc7 := archsimd.LoadUint32x8Slice(lastLane8x32Delta[:])
		iota8 := archsimd.LoadInt32x8Slice(laneIndexes[:])
		tail := iota8.Greater(zero)
		carry := zero
		negAcc := zero
		sumP := zero
		sumS := zero
		m := n / 8
		cp := unsafecast.Slice[[8]int32](prefix)[:m]
		cs := unsafecast.Slice[[8]int32](suffix)[:m]
		for j := range cp {
			p := archsimd.LoadInt32x8Slice(cp[j][:])
			s := archsimd.LoadInt32x8Slice(cs[j][:])
			negAcc = negAcc.Or(p).Or(s)
			lens := p.Add(s)
			lastLens := lens.Permute(rot).Merge(carry, tail)
			if p.Greater(lastLens).ToBits() != 0 {
				archsimd.ClearAVXUpperBits()
				return 0, 0, false
			}
			carry = lens.Permute(bc7)
			sumP = sumP.Add(p)
			sumS = sumS.Add(s)
		}
		if zero.Greater(negAcc).ToBits() != 0 {
			archsimd.ClearAVXUpperBits()
			return 0, 0, false
		}
		totalPrefixLength = int(reduceAddInt32x8(sumP))
		totalSuffixLength = int(reduceAddInt32x8(sumS))
		lastValueLength = carry.GetLo().GetElem(0)
		i = m * 8
		archsimd.ClearAVXUpperBits()
	}

	for ; i < n; i++ {
		p := prefix[i]
		s := suffix[i]
		if p < 0 || s < 0 || p > lastValueLength {
			return 0, 0, false
		}
		totalPrefixLength += int(p)
		totalSuffixLength += int(s)
		lastValueLength = p + s
	}

	if totalSuffixLength > maxLength {
		return 0, 0, false
	}
	return totalPrefixLength, totalSuffixLength, true
}

var (
	rotate1x8x32Delta = [8]uint32{7, 0, 1, 2, 3, 4, 5, 6}
	lastLane8x32Delta = [8]uint32{7, 7, 7, 7, 7, 7, 7, 7}
)

// searchPrefixLengthSIMD returns the length of the longest common prefix of
// base and data, comparing 32 bytes at a time; the first mismatching byte is
// recovered from the movemask of the byte equality.
func searchPrefixLengthSIMD(base, data []byte) int {
	n := min(len(base), len(data))
	m := n / 32
	bc := unsafecast.Slice[[32]byte](base)[:m]
	dc := unsafecast.Slice[[32]byte](data)[:m]

	for k := range bc {
		b := archsimd.LoadUint8x32(&bc[k])
		d := archsimd.LoadUint8x32(&dc[k])
		if eq := b.Equal(d).ToBits(); eq != 0xFFFFFFFF {
			archsimd.ClearAVXUpperBits()
			return k*32 + bits.TrailingZeros32(^eq)
		}
	}

	i := m * 32
	archsimd.ClearAVXUpperBits()
	return i + wordSearchPrefixLength(base[i:], data[i:])
}

func searchPrefixLength(base, data []byte) int {
	if archsimd.X86.AVX2() && min(len(base), len(data)) >= 32 {
		return searchPrefixLengthSIMD(base, data)
	}
	return wordSearchPrefixLength(base, data)
}

func reduceAddInt32x8(v archsimd.Int32x8) int32 {
	q := v.GetLo().Add(v.GetHi())
	p := q.AsFloat32x4().SelectFromPair(2, 3, 0, 1, q.AsFloat32x4()).AsInt32x4()
	q = q.Add(p)
	p = q.AsFloat32x4().SelectFromPair(1, 0, 3, 2, q.AsFloat32x4()).AsInt32x4()
	q = q.Add(p)
	return q.GetElem(0)
}

func validatePrefixAndSuffixLengthValues(prefix, suffix []int32, maxLength int) (totalPrefixLength, totalSuffixLength int, err error) {
	if archsimd.X86.AVX2() {
		totalPrefixLength, totalSuffixLength, ok := validatePrefixAndSuffixLengthValuesSIMD(prefix, suffix, maxLength)
		if ok {
			return totalPrefixLength, totalSuffixLength, nil
		}
	}

	lastValueLength := 0

	for i := range prefix {
		p := int(prefix[i])
		n := int(suffix[i])
		if p < 0 {
			err = errInvalidNegativePrefixLength(p)
			return
		}
		if n < 0 {
			err = errInvalidNegativeValueLength(n)
			return
		}
		if p > lastValueLength {
			err = errPrefixLengthOutOfBounds(p, lastValueLength)
			return
		}
		totalPrefixLength += p
		totalSuffixLength += n
		lastValueLength = p + n
	}

	if totalSuffixLength > maxLength {
		err = errValueLengthOutOfBounds(totalSuffixLength, maxLength)
		return
	}

	return totalPrefixLength, totalSuffixLength, nil
}

func decodeByteArrayOffsets(offsets []uint32, prefix, suffix []int32) {
	lastOffset := uint32(0)
	for i := range suffix {
		offsets[i] = lastOffset
		lastOffset += uint32(prefix[i]) + uint32(suffix[i])
	}
	offsets[len(suffix)] = lastOffset
}

// loadUint8x32 and storeUint8x32 access 32 bytes at arbitrary byte offsets
// of a slice without bounds checks; the callers guarantee that i+32 stays
// within the padding their buffers reserve.
func loadUint8x32(b []byte, i int) archsimd.Uint8x32 {
	return archsimd.LoadUint8x32((*[32]uint8)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), i)))
}

func storeUint8x32(v archsimd.Uint8x32, b []byte, i int) {
	v.Store((*[32]uint8)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), i)))
}

func loadUint8x16(b []byte, i int) archsimd.Uint8x16 {
	return archsimd.LoadUint8x16((*[16]uint8)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), i)))
}

func storeUint8x16(v archsimd.Uint8x16, b []byte, i int) {
	v.Store((*[16]uint8)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(b)), i)))
}

// decodeByteArraySIMD reconstructs values by copying prefixes and suffixes
// in unconditional 32 bytes chunks (over-copying into the padding the
// callers reserve). The caller guarantees at least padding bytes of suffix
// data remain in src past the region processed here, making the 32 bytes
// source loads safe.
//
// The loop advances raw pointer cursors instead of byte indexes: deriving
// each access from base+index costs an extra LEA per operation, which made
// the generated loop ~1.7x the instruction count of the equivalent
// hand-written assembly.
func decodeByteArraySIMD(dst, src []byte, prefix, suffix []int32) int {
	suffix = suffix[:len(prefix)]
	pd := unsafe.Pointer(unsafe.SliceData(dst))
	ps := unsafe.Pointer(unsafe.SliceData(src))
	pl := pd
	for k := range prefix {
		p := uintptr(uint32(prefix[k]))
		n := uintptr(uint32(suffix[k]))
		valueOffset := pd
		archsimd.LoadUint8x32((*[32]uint8)(pl)).Store((*[32]uint8)(pd))
		if p > 32 {
			for m := uintptr(32); m < p; m += 32 {
				archsimd.LoadUint8x32((*[32]uint8)(unsafe.Add(pl, m))).Store((*[32]uint8)(unsafe.Add(pd, m)))
			}
		}
		pd = unsafe.Add(pd, p)
		archsimd.LoadUint8x32((*[32]uint8)(ps)).Store((*[32]uint8)(pd))
		if n > 32 {
			for m := uintptr(32); m < n; m += 32 {
				archsimd.LoadUint8x32((*[32]uint8)(unsafe.Add(ps, m))).Store((*[32]uint8)(unsafe.Add(pd, m)))
			}
		}
		pd = unsafe.Add(pd, n)
		ps = unsafe.Add(ps, n)
		pl = valueOffset
	}
	archsimd.ClearAVXUpperBits()
	return int(uintptr(pd) - uintptr(unsafe.Pointer(unsafe.SliceData(dst))))
}

// decodeByteArray128SIMD is the specialization for fixed length 16 bytes
// values: the previous value stays in a vector register across iterations.
func decodeByteArray128SIMD(dst, src []byte, prefix, suffix []int32) int {
	suffix = suffix[:len(prefix)]
	i := 0
	j := 0
	last := loadUint8x16(dst, 0)
	for k := range prefix {
		p := int(prefix[k])
		n := int(suffix[k])
		storeUint8x16(last, dst, i)
		storeUint8x16(loadUint8x16(src, j), dst, i+p)
		last = loadUint8x16(dst, i)
		i += p + n
		j += n
	}
	archsimd.ClearAVXUpperBits()
	return i
}

func decodeByteArray(dst, src []byte, prefix, suffix []int32, offsets []uint32) ([]byte, []uint32, error) {
	totalPrefixLength, totalSuffixLength, err := validatePrefixAndSuffixLengthValues(prefix, suffix, len(src))
	if err != nil {
		return dst, offsets, err
	}

	totalLength := totalPrefixLength + totalSuffixLength
	dst = resizeNoMemclr(dst, totalLength+padding)

	if size := len(prefix) + 1; cap(offsets) < size {
		offsets = make([]uint32, size)
	} else {
		offsets = offsets[:size]
	}

	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]
	decodeByteArrayOffsets(offsets, prefix, suffix)

	var lastValue []byte
	var i int
	var j int

	if archsimd.X86.AVX2() && len(src) > padding {
		k := len(suffix)
		n := 0

		for k > 0 && n < padding {
			k--
			n += int(suffix[k])
		}

		if k > 0 && n >= padding {
			i = decodeByteArraySIMD(dst, src, prefix[:k], suffix[:k])
			j = len(src) - n
			lastValue = dst[i-(int(prefix[k-1])+int(suffix[k-1])):]
			prefix = prefix[k:]
			suffix = suffix[k:]
		}
	}

	for k := range prefix {
		p := int(prefix[k])
		n := int(suffix[k])
		lastValueOffset := i
		i += copy(dst[i:], lastValue[:p])
		i += copy(dst[i:], src[j:j+n])
		j += n
		lastValue = dst[lastValueOffset:]
	}

	return dst[:totalLength], offsets, nil
}

func decodeFixedLenByteArray(dst, src []byte, size int, prefix, suffix []int32) ([]byte, error) {
	totalPrefixLength, totalSuffixLength, err := validatePrefixAndSuffixLengthValues(prefix, suffix, len(src))
	if err != nil {
		return dst, err
	}

	totalLength := totalPrefixLength + totalSuffixLength
	dst = resizeNoMemclr(dst, totalLength+padding)

	_ = prefix[:len(suffix)]
	_ = suffix[:len(prefix)]

	var lastValue []byte
	var i int
	var j int

	if archsimd.X86.AVX2() && len(src) > padding {
		k := len(suffix)
		n := 0

		for k > 0 && n < padding {
			k--
			n += int(suffix[k])
		}

		if k > 0 && n >= padding {
			if size == 16 {
				i = decodeByteArray128SIMD(dst, src, prefix[:k], suffix[:k])
			} else {
				i = decodeByteArraySIMD(dst, src, prefix[:k], suffix[:k])
			}
			j = len(src) - n
			prefix = prefix[k:]
			suffix = suffix[k:]
			if i >= size {
				lastValue = dst[i-size:]
			}
		}
	}

	for k := range prefix {
		p := int(prefix[k])
		n := int(suffix[k])
		k := i
		i += copy(dst[i:], lastValue[:p])
		i += copy(dst[i:], src[j:j+n])
		j += n
		lastValue = dst[k:]
	}

	return dst[:totalLength], nil
}
