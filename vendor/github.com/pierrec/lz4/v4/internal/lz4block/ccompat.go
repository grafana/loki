package lz4block

import (
	"encoding/binary"
	"math/bits"
	"sync"
)

// CompressorCCompat is a port of LZ4_compress_fast from the reference
// implementation (lz4 1.10), and produces the same output. It searches one
// position per step with a 16kiB table, where Compressor searches three per
// step with a 128kiB one and skips ahead faster when matches stop.
//
// Constants and names follow lz4.c.
const (
	ccompatHashLog   = 12          // LZ4_HASHLOG, for the default LZ4_MEMORY_USAGE of 14
	ccompatMFLimit   = 12          // MFLIMIT
	ccompatMinLength = 13          // LZ4_minLength
	lastLiterals     = 5           // LASTLITERALS
	skipTrigger      = 6           // LZ4_skipTrigger
	ccompatU16Limit  = 64<<10 + 11 // LZ4_64Klimit: smaller inputs use the byU16 table
	ccompatMaxInput  = 0x7E000000  // LZ4_MAX_INPUT_SIZE
	distanceMax      = 65535       // LZ4_DISTANCE_MAX
	accelerationMax  = 65537       // LZ4_ACCELERATION_MAX

	ccompatTableSize = 1 << (ccompatHashLog + 1)
)

type CompressorCCompat struct {
	// Inputs below ccompatU16Limit use all of table16 (byU16 in lz4.c), larger
	// ones the first half of table32 (byU32). The one in use is cleared on
	// each call.
	table16 [ccompatTableSize]uint16
	table32 [ccompatTableSize]uint32
}

var compressorCCompatPool = sync.Pool{New: func() interface{} { return new(CompressorCCompat) }}

// CompressBlockCCompat is CompressorCCompat.CompressBlock with acceleration 1,
// using a pooled CompressorCCompat.
func CompressBlockCCompat(src, dst []byte) (int, error) {
	c := compressorCCompatPool.Get().(*CompressorCCompat)
	n, err := c.CompressBlock(src, dst, 1)
	compressorCCompatPool.Put(c)
	return n, err
}

// ccompatHasher is LZ4_hashPosition as (v<<shift)*prime>>bits, v being the
// 8 bytes at the position: with shift 32, only the low 4 bytes count, which
// is hash4 for byU16; with shift 24, hash5 for byU32.
type ccompatHasher struct {
	shift, bits uint
	prime       uint64
}

var (
	hash4 = ccompatHasher{shift: 32, prime: 2654435761, bits: 64 - (ccompatHashLog + 1)}
	hash5 = ccompatHasher{shift: 24, prime: 889523592379, bits: 64 - ccompatHashLog}
)

// hash returns a table index. Masking the shift counts spares the checks Go
// otherwise makes for shifts of 64 or more.
func (f ccompatHasher) hash(v uint64) uint32 {
	return uint32(v << (f.shift & 63) * f.prime >> (f.bits & 63))
}

// countMatch is LZ4_count: the length of the common prefix of src[in:limit]
// and src[match:], with match < in.
func countMatch(r srcReader, src []byte, in, match, limit int) int {
	start := in
	for in+8 <= limit {
		if diff := r.load64(src, match) ^ r.load64(src, in); diff != 0 {
			return in + bits.TrailingZeros64(diff)>>3 - start
		}
		in += 8
		match += 8
	}
	if in+4 <= limit && r.load32(src, match) == r.load32(src, in) {
		in += 4
		match += 4
	}
	if in+2 <= limit && r.load16(src, match) == r.load16(src, in) {
		in += 2
		match += 2
	}
	if in < limit && src[match] == src[in] {
		in++
	}
	return in - start
}

// CompressBlock compresses src into dst with the given acceleration, as
// LZ4_compress_fast_extState does. It returns 0 if the result does not fit
// in dst, which cannot happen if len(dst) >= CompressBlockBound(len(src)).
func (c *CompressorCCompat) CompressBlock(src, dst []byte, acceleration int) (int, error) {
	if acceleration < 1 {
		acceleration = 1
	} else if acceleration > accelerationMax {
		acceleration = accelerationMax
	}
	if len(src) == 0 {
		if len(dst) == 0 {
			return 0, nil
		}
		dst[0] = 0
		return 1, nil
	}
	if len(src) > ccompatMaxInput {
		// Beyond what lz4.c accepts, and what 32-bit positions can hold.
		return CompressBlock(src, dst)
	}
	if len(src) < ccompatU16Limit {
		c.table16 = [ccompatTableSize]uint16{}
		return compressCCompat(&c.table16, hash4, src, dst, acceleration), nil
	}
	t := c.table32[:1<<ccompatHashLog]
	for i := range t {
		t[i] = 0
	}
	return compressCCompat(&c.table32, hash5, src, dst, acceleration), nil
}

func compressCCompat[T uint16 | uint32](table *[ccompatTableSize]T, hr ccompatHasher, src, dst []byte, acceleration int) int {
	// limitedOutput in lz4.c: check that the output fits as it is written.
	limited := len(dst) < CompressBlockBound(len(src))
	// Loads are within the input: the positions searched stop ccompatMFLimit
	// bytes before its end, and matches before lastLiterals bytes.
	r := newSrcReader(src)

	var (
		anchor, ip, di int
		mfLimitPlusOne = len(src) - ccompatMFLimit + 1
		matchLimit     = len(src) - lastLiterals
		forwardH       uint32
		forwardV       uint64 // the 8 bytes at the next position to search
		match, token   int
	)
	if len(src) < ccompatMinLength {
		goto lastLiteralsLabel
	}

	// First byte.
	table[hr.hash(r.load64(src, 0))&(ccompatTableSize-1)] = 0
	ip = 1
	forwardV = r.load64(src, ip)
	forwardH = hr.hash(forwardV)

	for {
		// Find a match.
		{
			forwardIP := ip
			step := 1
			searchMatchNb := acceleration << skipTrigger
			for {
				h := forwardH
				cur := forwardIP
				curV := uint32(forwardV)
				matchIndex := int(table[h&(ccompatTableSize-1)])
				ip = forwardIP
				forwardIP += step
				step = searchMatchNb >> skipTrigger
				searchMatchNb++
				if forwardIP > mfLimitPlusOne {
					goto lastLiteralsLabel
				}
				match = matchIndex
				forwardV = r.load64(src, forwardIP)
				forwardH = hr.hash(forwardV)
				table[h&(ccompatTableSize-1)] = T(cur)
				// Never true for byU16 inputs, which are too short.
				if matchIndex+distanceMax < cur {
					continue // too far
				}
				if r.load32(src, match) == curV {
					break
				}
			}
		}

		// Catch up.
		if match > 0 && src[ip-1] == src[match-1] {
			for {
				ip--
				match--
				if ip <= anchor || match <= 0 || src[ip-1] != src[match-1] {
					break
				}
			}
		}

		// Encode literals.
		{
			litLength := ip - anchor
			token = di
			di++
			if limited && di+litLength+(2+1+lastLiterals)+litLength/255 > len(dst) {
				return 0
			}
			if litLength >= 0xF {
				dst[token] = 0xF0
				l := litLength - 0xF
				for ; l >= 0xFF; l -= 0xFF {
					dst[di] = 0xFF
					di++
				}
				dst[di] = byte(l)
				di++
			} else {
				dst[token] = byte(litLength << 4)
			}
			// LZ4_wildCopy8: may write up to 7 bytes past the literals,
			// which the rest of the output always overwrites.
			for i := 0; i < litLength; i += 8 {
				binary.LittleEndian.PutUint64(dst[di+i:], r.load64(src, anchor+i))
			}
			di += litLength
		}

	nextMatch:
		// Encode offset.
		offset := ip - match
		dst[di] = byte(offset)
		dst[di+1] = byte(offset >> 8)
		di += 2

		// Encode match length.
		{
			matchCode := countMatch(r, src, ip+minMatch, match+minMatch, matchLimit)
			ip += matchCode + minMatch
			if limited && di+(1+lastLiterals)+(matchCode+240)/255 > len(dst) {
				return 0
			}
			if matchCode >= 0xF {
				dst[token] += 0xF
				matchCode -= 0xF
				for ; matchCode >= 0xFF; matchCode -= 0xFF {
					dst[di] = 0xFF
					di++
				}
				dst[di] = byte(matchCode)
				di++
			} else {
				dst[token] += byte(matchCode)
			}
		}
		anchor = ip

		// Test end of chunk.
		if ip >= mfLimitPlusOne {
			break
		}

		// Fill table.
		table[hr.hash(r.load64(src, ip-2))&(ccompatTableSize-1)] = T(ip - 2)

		// Test next position.
		{
			v := r.load64(src, ip)
			h := hr.hash(v) & (ccompatTableSize - 1)
			matchIndex := int(table[h])
			table[h] = T(ip)
			if matchIndex+distanceMax >= ip && r.load32(src, matchIndex) == uint32(v) {
				token = di
				dst[di] = 0
				di++
				match = matchIndex
				goto nextMatch
			}
		}

		// Prepare next loop.
		ip++
		forwardV = r.load64(src, ip)
		forwardH = hr.hash(forwardV)
	}

lastLiteralsLabel:
	lastRun := len(src) - anchor
	if limited && di+lastRun+1+(lastRun+255-0xF)/255 > len(dst) {
		return 0
	}
	if lastRun >= 0xF {
		dst[di] = 0xF0
		di++
		acc := lastRun - 0xF
		for ; acc >= 0xFF; acc -= 0xFF {
			dst[di] = 0xFF
			di++
		}
		dst[di] = byte(acc)
		di++
	} else {
		dst[di] = byte(lastRun << 4)
		di++
	}
	di += copy(dst[di:], src[anchor:])
	return di
}
